// In-game vote panel (OpenLink, AGPL-3.0). Behaviour follows the retired OpenLink app's passive vote
// overlay (its overlay_windows.go and overlay_pad*.go, archived): a small always-on-top window drawn with
// GDI on its own thread that never takes focus, shows only while Halo Infinite (this process) is
// in front and a vote is open (plus the result for 8 s), and votes with the vote keys (default
// Alt+1-4), View + D-pad or a click. Its look follows Halo's own menus: on the server lobby screen
// it covers the lobby's greyed-out host rows (Mode editor .. Server), elsewhere it floats at the
// top left. Map thumbnails come from the Halo Waypoint image host. No sound.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <winhttp.h>
#include <wincodec.h>
#include <shlwapi.h>
#include "overlay.h"
#include "vote.h"
#include "hotkey.h"
#include <atomic>
#include <cstdio>
#include <cstring>
#include <string>
namespace clientjoin {
namespace overlay {
namespace {
constexpr UINT kWake=WM_APP+1;
constexpr UINT_PTR kRefreshTimer=1,kPadTimer=2;
constexpr int kThumbW=192,kThumbH=108,kMaxThumbSide=4096;
constexpr ULONGLONG kResultLinger=8000;
constexpr WORD kPadOrder[4]={0x0001,0x0008,0x0002,0x0004}; // up, right, down, left
constexpr WORD kPadView=0x0020;

std::atomic<bool> enabled{true},thread_started{false};
std::atomic<HWND> window{nullptr};
std::atomic<void (*)(const char*)> log_sink{nullptr};

std::wstring Wide(const char* utf8) {
    const int n=MultiByteToWideChar(CP_UTF8,0,utf8,-1,nullptr,0); if(n<=1) return L"";
    std::wstring s(size_t(n-1),L'\0'); MultiByteToWideChar(CP_UTF8,0,utf8,-1,&s[0],n); return s;
}
std::wstring Seconds(double s) { const int n=int((s<0 ? 0 : s)+0.999); return std::to_wstring(n)+L" s"; }

// ---- thumbnails (worker thread; cache shared with the window thread) ----
struct Thumb { char ref[80]; int w,h; uint32_t* bits; bool failed; };
constexpr size_t kMaxThumbs=32;
SRWLOCK thumb_lock=SRWLOCK_INIT;
Thumb thumbs[kMaxThumbs]={}; size_t thumb_count=0;
std::atomic<bool> fetching{false};
const Thumb* FindThumb(const char* ref) { for(size_t i=0;i<thumb_count;++i) if(!std::strcmp(thumbs[i].ref,ref)) return &thumbs[i]; return nullptr; }
bool Download(const wchar_t* path,std::string& body) {
    bool ok=false;
    HINTERNET session=WinHttpOpen(L"OpenLink-Launcher",WINHTTP_ACCESS_TYPE_AUTOMATIC_PROXY,WINHTTP_NO_PROXY_NAME,WINHTTP_NO_PROXY_BYPASS,0);
    if(!session) return false;
    WinHttpSetTimeouts(session,8000,8000,8000,8000);
    HINTERNET connection=WinHttpConnect(session,L"blobs-infiniteugc.svc.halowaypoint.com",INTERNET_DEFAULT_HTTPS_PORT,0);
    HINTERNET request=connection ? WinHttpOpenRequest(connection,L"GET",path,nullptr,WINHTTP_NO_REFERER,WINHTTP_DEFAULT_ACCEPT_TYPES,WINHTTP_FLAG_SECURE) : nullptr;
    if(request && WinHttpSendRequest(request,WINHTTP_NO_ADDITIONAL_HEADERS,0,WINHTTP_NO_REQUEST_DATA,0,0,0) && WinHttpReceiveResponse(request,nullptr)) {
        DWORD code=0,size=sizeof(code);
        if(WinHttpQueryHeaders(request,WINHTTP_QUERY_STATUS_CODE|WINHTTP_QUERY_FLAG_NUMBER,WINHTTP_HEADER_NAME_BY_INDEX,&code,&size,WINHTTP_NO_HEADER_INDEX) && code==200) {
            ok=true; char chunk[16384]; DWORD got=0;
            while(WinHttpReadData(request,chunk,sizeof(chunk),&got) && got) {
                if(body.size()+got>(4u<<20)) { ok=false; break; } // 4 MiB cap
                body.append(chunk,got);
            }
            ok=ok && !body.empty();
        }
    }
    if(request) WinHttpCloseHandle(request); if(connection) WinHttpCloseHandle(connection); WinHttpCloseHandle(session);
    return ok;
}
// Decodes an image, centre-crops it to 16:9 and scales it to at most 192x108 (never larger).
uint32_t* Decode(IWICImagingFactory* wic,const std::string& data,int& w,int& h) {
    uint32_t* out=nullptr;
    IStream* stream=SHCreateMemStream(reinterpret_cast<const BYTE*>(data.data()),UINT(data.size()));
    IWICBitmapDecoder* decoder=nullptr; IWICBitmapFrameDecode* frame=nullptr; IWICBitmapClipper* clipper=nullptr;
    IWICBitmapScaler* scaler=nullptr; IWICFormatConverter* converter=nullptr;
    UINT sw=0,sh=0;
    if(stream && SUCCEEDED(wic->CreateDecoderFromStream(stream,nullptr,WICDecodeMetadataCacheOnDemand,&decoder)) && SUCCEEDED(decoder->GetFrame(0,&frame)) &&
       SUCCEEDED(frame->GetSize(&sw,&sh)) && sw && sh && sw<=kMaxThumbSide && sh<=kMaxThumbSide) {
        WICRect crop={0,0,INT(sw),INT(sh)};
        if(UINT64(sw)*9>UINT64(sh)*16) { crop.Width=INT(UINT64(sh)*16/9); crop.X=(INT(sw)-crop.Width)/2; }
        else { crop.Height=INT(UINT64(sw)*9/16); crop.Y=(INT(sh)-crop.Height)/2; }
        if(crop.Width<1) crop.Width=1; if(crop.Height<1) crop.Height=1;
        int tw=crop.Width<kThumbW ? crop.Width : kThumbW, th=crop.Height<kThumbH ? crop.Height : kThumbH;
        if(tw*9<th*16) th=tw*9/16; else tw=th*16/9;
        if(tw<1) tw=1; if(th<1) th=1;
        if(SUCCEEDED(wic->CreateBitmapClipper(&clipper)) && SUCCEEDED(clipper->Initialize(frame,&crop)) &&
           SUCCEEDED(wic->CreateBitmapScaler(&scaler)) && SUCCEEDED(scaler->Initialize(clipper,UINT(tw),UINT(th),WICBitmapInterpolationModeFant)) &&
           SUCCEEDED(wic->CreateFormatConverter(&converter)) &&
           SUCCEEDED(converter->Initialize(scaler,GUID_WICPixelFormat32bppBGR,WICBitmapDitherTypeNone,nullptr,0,WICBitmapPaletteTypeCustom))) {
            out=static_cast<uint32_t*>(HeapAlloc(GetProcessHeap(),0,size_t(tw)*size_t(th)*4));
            if(out && FAILED(converter->CopyPixels(nullptr,UINT(tw*4),UINT(tw*th*4),reinterpret_cast<BYTE*>(out)))) { HeapFree(GetProcessHeap(),0,out); out=nullptr; }
            if(out) { w=tw; h=th; }
        }
    }
    if(converter) converter->Release(); if(scaler) scaler->Release(); if(clipper) clipper->Release();
    if(frame) frame->Release(); if(decoder) decoder->Release(); if(stream) stream->Release();
    return out;
}
DWORD WINAPI FetchThumbs(void*) {
    const HRESULT com=CoInitializeEx(nullptr,COINIT_MULTITHREADED);
    IWICImagingFactory* wic=nullptr;
    CoCreateInstance(CLSID_WICImagingFactory,nullptr,CLSCTX_INPROC_SERVER,IID_PPV_ARGS(&wic));
    for(;;) {
        vote::View v={}; if(!vote::Current(v)) break;
        char ref[80]={};
        AcquireSRWLockShared(&thumb_lock);
        for(uint32_t i=0;i<v.ballot.count && !ref[0];++i) if(v.ballot.options[i].thumb[0] && !FindThumb(v.ballot.options[i].thumb)) strcpy_s(ref,v.ballot.options[i].thumb);
        ReleaseSRWLockShared(&thumb_lock);
        if(!ref[0]) break;
        Thumb t={}; strcpy_s(t.ref,ref); t.failed=true;
        if(wic) for(const wchar_t* ext:{L"jpg",L"png"}) {
            wchar_t path[200]; swprintf_s(path,L"/ugcstorage/map/%hs/images/thumbnail.%ls",ref,ext);
            std::string body;
            if(Download(path,body) && (t.bits=Decode(wic,body,t.w,t.h))!=nullptr) { t.failed=false; break; }
        }
        AcquireSRWLockExclusive(&thumb_lock);
        if(thumb_count>=kMaxThumbs) { for(size_t i=0;i<thumb_count;++i) if(thumbs[i].bits) HeapFree(GetProcessHeap(),0,thumbs[i].bits); thumb_count=0; }
        thumbs[thumb_count++]=t;
        ReleaseSRWLockExclusive(&thumb_lock);
        if(const HWND h=window.load()) PostMessageW(h,kWake,0,0);
    }
    if(wic) wic->Release();
    if(SUCCEEDED(com)) CoUninitialize();
    fetching.store(false);
    return 0;
}
void StartFetch(const vote::View& v) {
    bool missing=false;
    AcquireSRWLockShared(&thumb_lock);
    for(uint32_t i=0;i<v.ballot.count;++i) if(v.ballot.options[i].thumb[0] && !FindThumb(v.ballot.options[i].thumb)) missing=true;
    ReleaseSRWLockShared(&thumb_lock);
    bool expected=false;
    if(!missing || !fetching.compare_exchange_strong(expected,true)) return;
    HANDLE t=CreateThread(nullptr,0,FetchThumbs,nullptr,0,nullptr);
    if(t) CloseHandle(t); else fetching.store(false);
}

// ---- window thread state ----
bool visible=false;
uint64_t closed_round=0; ULONGLONG closed_at=0;
bool registered[4]={}; wchar_t key_error[96]={};
bool pad_timer=false,pad_seen=false,pad_conn[4]={}; WORD pad_prev[4]={}; ULONGLONG pad_next[4]={};
int hover=-1; bool mouse_tracked=false; // the choice under the mouse (-1: none)
using XInputGetStateFn=DWORD (WINAPI*)(DWORD,void*);
XInputGetStateFn xinput=nullptr;
std::atomic<bool> lobby_block{false}; // the server lobby screen is showing (set from the UI script)
std::atomic<bool> held{false};        // hidden while the "Leave server?" dialog and the leave run

bool GameInFront(HWND self) {
    const HWND fg=GetForegroundWindow(); if(!fg) return false; if(fg==self) return true;
    DWORD pid=0; GetWindowThreadProcessId(fg,&pid); return pid==GetCurrentProcessId();
}
void Hide(HWND h) { if(visible) { ShowWindow(h,SW_HIDE); visible=false; } hover=-1; }
// Vote keys are held only while a vote is open (default Alt+1..n); keys another app holds are noted.
hotkey::Key vote_keys[hotkey::kVoteKeys]={}; bool vote_keys_set=false; // set once before the overlay thread starts
const hotkey::Key& VoteKey(uint32_t i) {
    if(!vote_keys_set) { hotkey::DefaultVoteKeys(vote_keys); vote_keys_set=true; }
    return vote_keys[i];
}
std::wstring WideName(const char* s) { std::wstring w; for(;*s;++s) w+=wchar_t(static_cast<unsigned char>(*s)); return w; } // ASCII key names
void SetKeys(HWND h,uint32_t n) {
    std::wstring taken;
    for(uint32_t i=0;i<4;++i) {
        const bool want=i<n;
        if(want && !registered[i]) {
            registered[i]=RegisterHotKey(h,int(i+1),VoteKey(i).mods|MOD_NOREPEAT,VoteKey(i).vk)!=FALSE;
            if(!registered[i]) { if(!taken.empty()) taken+=L", "; taken+=WideName(VoteKey(i).name); }
        }
        else if(!want && registered[i]) { UnregisterHotKey(h,int(i+1)); registered[i]=false; }
    }
    if(n==0) key_error[0]=0;
    else if(!taken.empty()) swprintf_s(key_error,L"%ls in use by another app",taken.c_str());
}
void SetPadTimer(HWND h,bool on) {
    on=on && xinput;
    if(on==pad_timer) return;
    pad_timer=on;
    if(on) SetTimer(h,kPadTimer,33,nullptr); else { KillTimer(h,kPadTimer); pad_seen=false; }
}
void Refresh(HWND h);
void Choose(HWND h,int i) {
    if(!GameInFront(h)) return;
    vote::Cast(i);
    Refresh(h);
}
// View + a newly pressed D-pad direction votes (up, right, down, left = choices 1-4).
void PollPads(HWND h) {
    vote::View v={}; const bool open=vote::Current(v) && !v.ballot.closed;
    const ULONGLONG now=GetTickCount64(); bool seen=false;
    for(DWORD i=0;i<4;++i) {
        if(now<pad_next[i]) { seen=seen || pad_conn[i]; continue; } // empty slot, checked again later
        struct { DWORD packet; WORD buttons; BYTE lt,rt; SHORT lx,ly,rx,ry; } state={};
        if(xinput(i,&state)!=ERROR_SUCCESS) { pad_conn[i]=false; pad_prev[i]=0; pad_next[i]=now+1000; continue; }
        pad_conn[i]=true; seen=true;
        const WORD prev=pad_prev[i],cur=state.buttons; pad_prev[i]=cur;
        if(!open || !(cur&kPadView)) continue;
        const WORD pressed=WORD(cur&~prev);
        for(uint32_t k=0;k<4 && k<v.ballot.count;++k) if(pressed&kPadOrder[k]) { Choose(h,int(k)); break; }
    }
    if(seen!=pad_seen) { pad_seen=seen; Refresh(h); }
}

// ---- drawing: the look of Halo's own menus ----
// Near-black panels, thin grey rules, wide-spaced capitals in a DIN-like face (Bahnschrift is
// the closest Windows font) and a grey fill with a white outline for the focused row. Sizes are
// in units: 248 units are the height of the lobby's four stock menu rows.
constexpr COLORREF kHaloBg=RGB(0x0c,0x11,0x15),kHaloRule=RGB(0x3c,0x42,0x47),kHaloText=RGB(0xe8,0xeb,0xec),
    kHaloMuted=RGB(0x9a,0xa1,0xa5),kHaloDim=RGB(0x6b,0x72,0x76),kHaloFocus=RGB(0x40,0x45,0x4a),kHaloOutline=RGB(0xf2,0xf4,0xf5),
    kHaloBlue=RGB(0x3b,0x8f,0xdc),kHaloWarn=RGB(0xff,0xb5,0x47);
const wchar_t* const kPadWords[4]={L"VIEW+UP",L"VIEW+RIGHT",L"VIEW+DOWN",L"VIEW+LEFT"}; // Bahnschrift has no arrows
void Fill(HDC dc,RECT r,COLORREF c) { HBRUSH b=CreateSolidBrush(c); FillRect(dc,&r,b); DeleteObject(b); }
void Image(HDC dc,const Thumb& t,RECT r) {
    BITMAPINFO bi={}; bi.bmiHeader.biSize=sizeof(bi.bmiHeader); bi.bmiHeader.biWidth=t.w; bi.bmiHeader.biHeight=-t.h;
    bi.bmiHeader.biPlanes=1; bi.bmiHeader.biBitCount=32;
    SetStretchBltMode(dc,HALFTONE); SetBrushOrgEx(dc,0,0,nullptr);
    StretchDIBits(dc,r.left,r.top,r.right-r.left,r.bottom-r.top,0,0,t.w,t.h,t.bits,&bi,DIB_RGB_COLORS,SRCCOPY);
}
std::wstring Upper(std::wstring s) { if(!s.empty()) CharUpperBuffW(&s[0],DWORD(s.size())); return s; }
HFONT HaloFont(int px_size,int weight) { return CreateFontW(-px_size,0,0,0,weight,0,0,0,DEFAULT_CHARSET,0,0,CLEARTYPE_QUALITY,0,L"Bahnschrift"); }
void Spaced(HDC dc,const std::wstring& s,RECT r,HFONT f,int spacing,COLORREF c,UINT flags) {
    HGDIOBJ old=SelectObject(dc,f); SetTextColor(dc,c); const int before=SetTextCharacterExtra(dc,spacing);
    DrawTextW(dc,s.c_str(),int(s.size()),&r,flags|DT_SINGLELINE|DT_NOPREFIX); SetTextCharacterExtra(dc,before); SelectObject(dc,old);
}
int SpacedWidth(HDC dc,const std::wstring& s,HFONT f,int spacing) {
    HGDIOBJ old=SelectObject(dc,f); const int before=SetTextCharacterExtra(dc,spacing);
    SIZE sz={}; GetTextExtentPoint32W(dc,s.c_str(),int(s.size()),&sz); SetTextCharacterExtra(dc,before); SelectObject(dc,old); return sz.cx;
}
// Shortens s with "…" until it fits max_w (DT_END_ELLIPSIS ignores the extra spacing and clips).
std::wstring Fit(HDC dc,std::wstring s,HFONT f,int spacing,int max_w) {
    if(SpacedWidth(dc,s,f,spacing)<=max_w) return s;
    while(!s.empty()) { s.pop_back(); while(!s.empty() && s.back()==L' ') s.pop_back(); if(SpacedWidth(dc,s+L"\x2026",f,spacing)<=max_w) return s+L"\x2026"; }
    return s;
}
void Box(HDC dc,RECT r,COLORREF fill,COLORREF border,int width) {
    Fill(dc,r,fill);
    if(width>0) { Fill(dc,{r.left,r.top,r.right,r.top+width},border); Fill(dc,{r.left,r.bottom-width,r.right,r.bottom},border);
                  Fill(dc,{r.left,r.top,r.left+width,r.bottom},border); Fill(dc,{r.right-width,r.top,r.right,r.bottom},border); }
}
constexpr double kHeadUnits=30,kTopGap=6,kRowUnits=62;
// Height in units of a panel for n choices that is not tied to the lobby block (floating).
double FloatingUnits(uint32_t n) { return kHeadUnits+kTopGap+double(n)*kRowUnits; }
// Where the rows are in a panel h pixels tall: the first row's top and the row pitch (painting and clicks).
struct RowLayout { int top,row; };
RowLayout Rows(int h,double unit,bool fill,int n) {
    auto U=[unit](double units) { return int(units*unit+0.5); };
    const int top=U(kHeadUnits)+U(kTopGap),avail=h-top;
    return {top,n ? (fill || avail/n<U(kRowUnits) ? avail/n : U(kRowUnits)) : 0};
}
// fill: the rows share the panel's whole height (lobby); otherwise each is at most kRowUnits tall.
void PaintHalo(HDC dc,const vote::View& v,int w,int h,double unit,bool fill) {
    const relay::Ballot& b=v.ballot;
    auto U=[unit](double units) { return int(units*unit+0.5); };
    Fill(dc,{0,0,w,h},kHaloBg);
    HFONT head=HaloFont(U(17),700),minor=HaloFont(U(13),500),tag=HaloFont(U(13),700);
    // Header, like the lobby's "DETAILS" title: capitals, wide spacing, a rule underneath.
    std::wstring title,timer; COLORREF timer_col=kHaloBlue;
    if(b.closed) { title=L"NEXT MATCH"; timer=v.starts_in_s>=0 ? L"STARTING IN "+Upper(Seconds(v.starts_in_s)) : L"STARTING SOON"; timer_col=kHaloText; }
    else { title=L"VOTE FOR THE NEXT MATCH"; timer=Upper(Seconds(v.remaining_s))+L" LEFT"; }
    if(v.error[0]) { timer=L"VOTE NOT SENT"; timer_col=kHaloWarn; }
    else if(!b.closed && key_error[0]) { timer=L"KEYS IN USE: USE VIEW+D-PAD"; timer_col=kHaloWarn; }
    const int head_h=U(kHeadUnits);
    const int timer_w=SpacedWidth(dc,timer,minor,U(1.2));
    Spaced(dc,Fit(dc,title,head,U(2.4),w-timer_w-U(16)),{U(4),0,w,head_h-U(4)},head,U(2.4),kHaloText,DT_VCENTER);
    Spaced(dc,timer,{w-U(4)-timer_w,0,w,head_h-U(4)},minor,U(1.2),timer_col,DT_VCENTER);
    Fill(dc,{0,head_h-U(2),w,head_h},kHaloRule);
    const int n=int(b.count);
    const RowLayout layout=Rows(h,unit,fill,n); const int top=layout.top,row=layout.row;
    const int name_px=row*36/100<U(20) ? row*36/100 : U(20); // smaller names when four rows share the block
    HFONT name=HaloFont(name_px,600);
    int total=0; for(int i=0;i<n;++i) total+=b.counts[i];
    AcquireSRWLockShared(&thumb_lock);
    for(int i=0;i<n;++i) {
        const RECT r={0,top+i*row,w,top+(i+1)*row-U(4)};
        const bool mine=!b.closed && i==v.mine, winner=b.closed && i==b.winner, faded=b.closed && !winner;
        // Your vote / the winner: Halo's focused row (fill + outline); under the mouse: the fill only.
        if(mine || winner) Box(dc,r,kHaloFocus,kHaloOutline,U(1.6));
        else {
            if(!b.closed && i==hover) Fill(dc,r,kHaloFocus);
            Fill(dc,{r.left,r.bottom-U(1),r.right,r.bottom},kHaloRule);
        }
        // Map art, 16:9, inset in the row.
        const int ih=(r.bottom-r.top)-U(8), iw=ih*16/9;
        const RECT art={r.left+U(5),r.top+U(4),r.left+U(5)+iw,r.top+U(4)+ih};
        Fill(dc,art,RGB(0x1a,0x20,0x24));
        if(const Thumb* t=b.options[i].thumb[0] ? FindThumb(b.options[i].thumb) : nullptr) if(t->bits) Image(dc,*t,art);
        const int tx=art.right+U(12), right=r.right-U(10);
        // Right: vote count; left of it the name in capitals, and under it the keys or the result.
        const std::wstring votes=b.counts[i]==1 ? L"1 VOTE" : std::to_wstring(b.counts[i])+L" VOTES";
        const int votes_w=SpacedWidth(dc,votes,minor,U(1));
        const int name_mid=r.top+(r.bottom-r.top)*11/20;
        // A long name first shrinks (to 80%) to fit, then is cut with an ellipsis.
        const std::wstring label=Upper(Wide(b.options[i].name));
        const int name_w=right-votes_w-U(12)-tx, full_w=SpacedWidth(dc,label,name,U(1.6));
        HFONT fitted=nullptr;
        if(name_w>0 && full_w>name_w) { const int px=name_px*name_w/full_w; fitted=HaloFont(px>name_px*4/5 ? px : name_px*4/5,600); }
        HFONT nf=fitted ? fitted : name;
        Spaced(dc,Fit(dc,label,nf,U(1.6),name_w),{tx,r.top,right-votes_w-U(12),name_mid},nf,U(1.6),faded ? kHaloDim : kHaloText,DT_BOTTOM);
        if(fitted) DeleteObject(fitted);
        std::wstring hint;
        if(winner) hint=L"NEXT";
        else if(!b.closed) { hint=Upper(WideName(VoteKey(uint32_t(i)).name)); if(pad_seen) hint+=std::wstring(L"  \x00b7  ")+kPadWords[i]; if(mine) hint+=L"  \x00b7  YOUR VOTE"; }
        Spaced(dc,Fit(dc,hint,winner ? tag : minor,U(1.2),right-tx),{tx,name_mid+U(3),right,r.bottom-U(2)},winner ? tag : minor,U(1.2),winner ? kHaloBlue : (mine ? kHaloText : kHaloMuted),DT_TOP);
        Spaced(dc,votes,{right-votes_w,r.top,right+U(8),r.bottom},minor,U(1),faded ? kHaloDim : kHaloMuted,DT_VCENTER);
        // Thin vote meter along the bottom of the row.
        if(total>0 && b.counts[i]>0) Fill(dc,{art.right,r.bottom-U(3),art.right+(r.right-art.right)*b.counts[i]/total,r.bottom-U(1)},faded ? kHaloRule : kHaloBlue);
    }
    ReleaseSRWLockShared(&thumb_lock);
    for(HFONT f:{head,name,minor,tag}) DeleteObject(f);
}

// ---- placement ----
// Halo lays its menus out on a 1920x1080 canvas scaled uniformly into the window and centred
// (ultrawide: bars left and right). The lobby's four stock menu rows (Mode editor .. Server)
// fill this canvas rectangle (measured on a 3440x1440 lobby screenshot).
constexpr RECT kLobbyBlock={95,495,626,746};
// The panel reaches right to just short of the DETAILS column and left over the start button's
// focus bracket; with 3-4 choices it also covers the greyed start button (players on a
// server-owned lobby cannot start a match), stopping above the button's description line, which
// is wider than the panel. Measured on the same screenshot.
constexpr LONG kPanelLeft=87,kPanelRight=675,kTallBottom=850;
// Panel units per canvas unit (the drawing scale).
constexpr double kUnitPerCanvas=1.0;
// The panel over the stock rows `rows` (k pixels per canvas unit).
RECT LobbyPanel(RECT rows,double k,uint32_t count) {
    return {LONG(rows.left-(kLobbyBlock.left-kPanelLeft)*k+0.5),rows.top,LONG(rows.right+(kPanelRight-kLobbyBlock.right)*k+0.5),
            count>=3 ? LONG(rows.bottom+(kTallBottom-kLobbyBlock.bottom)*k+0.5) : rows.bottom};
}
struct Canvas { double x,y,k; }; // screen origin of the canvas and pixels per canvas unit
Canvas CanvasOf(HWND game) {
    RECT c={}; GetClientRect(game,&c); POINT o={0,0}; ClientToScreen(game,&o);
    const double cw=c.right-c.left,ch=c.bottom-c.top;
    double k=ch/1080.0; if(cw<k*1920.0) k=cw/1920.0;
    return {o.x+(cw-1920.0*k)/2,o.y+(ch-1080.0*k)/2,k};
}
// The rows' rectangle from the UI script (UI units, FN068), checked once per lobby screen against
// the measured block above. UI units are taken as client pixels (unverified; logged).
SRWLOCK lobby_lock=SRWLOCK_INIT;
float lobby_rect[5]={}; bool lobby_log=false;
void Place(HWND h,const vote::View& v,RECT& rect,double& unit,bool& fill) {
    const HWND game=GetForegroundWindow();
    const Canvas c=CanvasOf(game);
    unit=c.k*kUnitPerCanvas;
    fill=lobby_block.load();
    if(fill) {
        // Over the lobby's greyed host rows: the vote takes their place.
        const RECT measured={LONG(c.x+kLobbyBlock.left*c.k),LONG(c.y+kLobbyBlock.top*c.k),LONG(c.x+kLobbyBlock.right*c.k),LONG(c.y+kLobbyBlock.bottom*c.k)};
        float r[5]; bool log=false;
        AcquireSRWLockExclusive(&lobby_lock); std::memcpy(r,lobby_rect,sizeof(r)); log=lobby_log; lobby_log=false; ReleaseSRWLockExclusive(&lobby_lock);
        RECT client={}; GetClientRect(game,&client); POINT o={0,0}; ClientToScreen(game,&o);
        const float cw=float(client.right),ch=float(client.bottom);
        const bool plausible=r[2]>r[0] && r[3]>r[1] && r[0]>=0 && r[1]>=0 && r[2]<=cw && r[3]<=ch && r[2]-r[0]>cw*0.05f && r[3]-r[1]>ch*0.05f;
        rect=LobbyPanel(plausible ? RECT{LONG(o.x+r[0]),LONG(o.y+r[1]),LONG(o.x+r[2]),LONG(o.y+r[3])} : measured,c.k,v.ballot.count);
        if(log) {
            char line[256];
            sprintf_s(line,"vote panel: client %.0fx%.0f, ui rows %.1f,%.1f,%.1f,%.1f (button bottom %.1f), measured %ld,%ld,%ld,%ld -> using %s",
                      cw,ch,r[0],r[1],r[2],r[3],r[4],measured.left-o.x,measured.top-o.y,measured.right-o.x,measured.bottom-o.y,plausible ? "ui" : "measured");
            if(const auto sink=log_sink.load()) sink(line);
        }
    } else {
        // Anywhere else (fallback): the same panel at the top left, as tall as its choices.
        const LONG x=LONG(c.x+24*c.k),y=LONG(c.y+24*c.k);
        rect={x,y,LONG(x+(kPanelRight-kLobbyBlock.left)*c.k),LONG(y+FloatingUnits(v.ballot.count)*unit)};
    }
    SetWindowPos(h,HWND_TOPMOST,rect.left,rect.top,rect.right-rect.left,rect.bottom-rect.top,SWP_NOACTIVATE|SWP_SHOWWINDOW);
    visible=true;
    InvalidateRect(h,nullptr,FALSE);
}
RECT placed={}; double placed_unit=1; bool placed_fill=false;
void Refresh(HWND h) {
    vote::View v={};
    if(!enabled.load() || held.load() || !vote::Current(v) || !v.ballot.count) { SetKeys(h,0); SetPadTimer(h,false); Hide(h); return; }
    SetKeys(h,v.ballot.closed ? 0 : v.ballot.count);
    SetPadTimer(h,!v.ballot.closed);
    if(v.ballot.closed) {
        if(closed_round!=v.ballot.round) { closed_round=v.ballot.round; closed_at=GetTickCount64(); }
        if(GetTickCount64()-closed_at>kResultLinger) { Hide(h); return; }
    }
    if(!GameInFront(h)) { Hide(h); return; }
    Place(h,v,placed,placed_unit,placed_fill);
    StartFetch(v);
}
void Paint(HWND h) {
    PAINTSTRUCT ps; HDC hdc=BeginPaint(h,&ps);
    RECT cr; GetClientRect(h,&cr);
    if(cr.right>0 && cr.bottom>0) {
        HDC mem=CreateCompatibleDC(hdc); HBITMAP bmp=CreateCompatibleBitmap(hdc,cr.right,cr.bottom);
        HGDIOBJ old=SelectObject(mem,bmp); SetBkMode(mem,TRANSPARENT);
        Fill(mem,cr,kHaloBg);
        vote::View v={};
        if(vote::Current(v) && v.ballot.count) PaintHalo(mem,v,cr.right,cr.bottom,placed_unit,placed_fill);
        BitBlt(hdc,0,0,cr.right,cr.bottom,mem,0,0,SRCCOPY);
        SelectObject(mem,old); DeleteObject(bmp); DeleteDC(mem);
    }
    EndPaint(h,&ps);
}
// The open ballot's choice at client y, or -1.
int RowAt(HWND h,int y) {
    vote::View v={};
    if(!vote::Current(v) || v.ballot.closed || !v.ballot.count) return -1;
    RECT cr; GetClientRect(h,&cr);
    const RowLayout l=Rows(cr.bottom,placed_unit,placed_fill,int(v.ballot.count));
    if(y<l.top || l.row<=0) return -1;
    const int i=(y-l.top)/l.row;
    return i<int(v.ballot.count) ? i : -1;
}
void SetHover(HWND h,int i) { if(i!=hover) { hover=i; InvalidateRect(h,nullptr,FALSE); } }
void MouseMove(HWND h,int y) {
    if(!mouse_tracked) { TRACKMOUSEEVENT t={sizeof(t),TME_LEAVE,h,0}; mouse_tracked=TrackMouseEvent(&t)!=FALSE; }
    SetHover(h,RowAt(h,y));
}
void Click(HWND h,int y) {
    const int i=RowAt(h,y); if(i<0) return;
    char line[64]; sprintf_s(line,"vote panel: mouse vote for choice %d",i+1);
    if(const auto sink=log_sink.load()) sink(line);
    Choose(h,i);
}
LRESULT CALLBACK Proc(HWND h,UINT msg,WPARAM wp,LPARAM lp) {
    switch(msg) {
    // Clicks vote without taking focus from the game; the arrow is set here because the game may hide
    // the system cursor.
    case WM_MOUSEACTIVATE: return MA_NOACTIVATE;
    case WM_SETCURSOR: SetCursor(LoadCursorW(nullptr,MAKEINTRESOURCEW(32512))); return TRUE; // IDC_ARROW
    case WM_MOUSEMOVE: MouseMove(h,short(HIWORD(lp))); return 0;
    case WM_MOUSELEAVE: mouse_tracked=false; SetHover(h,-1); return 0;
    case WM_LBUTTONDOWN: Click(h,short(HIWORD(lp))); return 0;
    case WM_ERASEBKGND: return 1;
    case WM_PAINT: Paint(h); return 0;
    case WM_TIMER: if(wp==kPadTimer) PollPads(h); else Refresh(h); return 0;
    case WM_HOTKEY: if(wp>=1 && wp<=4) Choose(h,int(wp-1)); return 0;
    case kWake: Refresh(h); return 0;
    case WM_CLOSE: return 0; // stays for the life of the game
    }
    return DefWindowProcW(h,msg,wp,lp);
}
DWORD WINAPI Thread(void*) {
    // Window positions are in physical pixels whatever the game's own DPI setting.
    using SetContextFn=DPI_AWARENESS_CONTEXT (WINAPI*)(DPI_AWARENESS_CONTEXT);
    if(const auto set=reinterpret_cast<SetContextFn>(GetProcAddress(GetModuleHandleW(L"user32.dll"),"SetThreadDpiAwarenessContext")))
        set(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    for(const wchar_t* name:{L"xinput1_4.dll",L"xinput9_1_0.dll"})
        if(HMODULE x=LoadLibraryW(name)) if((xinput=reinterpret_cast<XInputGetStateFn>(GetProcAddress(x,"XInputGetState")))!=nullptr) break;
    HMODULE self=nullptr;
    GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,reinterpret_cast<LPCWSTR>(&Wake),&self);
    WNDCLASSEXW wc={}; wc.cbSize=sizeof(wc); wc.lpfnWndProc=Proc; wc.hInstance=self; wc.hCursor=LoadCursorW(nullptr,MAKEINTRESOURCEW(32512)); // IDC_ARROW
    wc.lpszClassName=L"OpenLinkLauncherVoteOverlay";
    if(!RegisterClassExW(&wc)) return GetLastError();
    HWND h=CreateWindowExW(WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_LAYERED|WS_EX_NOACTIVATE,wc.lpszClassName,L"OpenLink vote",
                           WS_POPUP,0,0,0,0,nullptr,nullptr,self,nullptr);
    if(!h) return GetLastError();
    SetLayeredWindowAttributes(h,0,255,LWA_ALPHA); // opaque: it covers the lobby's greyed rows
    window.store(h);
    SetTimer(h,kRefreshTimer,500,nullptr);
    Refresh(h);
    MSG m; while(GetMessageW(&m,nullptr,0,0)>0) { TranslateMessage(&m); DispatchMessageW(&m); }
    return 0;
}
}
// ---- development previews (no window, no game): panel or lobby style into a PNG ----
namespace {
void FetchNow(IWICImagingFactory* wic,const vote::View& v) {
    for(uint32_t i=0;i<v.ballot.count;++i) {
        const char* ref=v.ballot.options[i].thumb; if(!ref[0] || FindThumb(ref) || thumb_count>=kMaxThumbs) continue;
        Thumb t={}; strcpy_s(t.ref,ref); t.failed=true;
        for(const wchar_t* ext:{L"jpg",L"png"}) {
            wchar_t p[200]; swprintf_s(p,L"/ugcstorage/map/%hs/images/thumbnail.%ls",ref,ext);
            std::string body; if(Download(p,body) && (t.bits=Decode(wic,body,t.w,t.h))!=nullptr) { t.failed=false; break; }
        }
        thumbs[thumb_count++]=t;
    }
}
bool SavePng(IWICImagingFactory* wic,uint32_t* bits,int w,int h,const wchar_t* path) {
    for(int i=0;i<w*h;++i) bits[i]|=0xff000000u; // GDI leaves alpha 0: make it opaque
    IWICStream* stream=nullptr; IWICBitmapEncoder* encoder=nullptr; IWICBitmapFrameEncode* frame=nullptr;
    WICPixelFormatGUID format=GUID_WICPixelFormat32bppBGRA;
    const bool ok=SUCCEEDED(wic->CreateStream(&stream)) && SUCCEEDED(stream->InitializeFromFilename(path,GENERIC_WRITE)) &&
       SUCCEEDED(wic->CreateEncoder(GUID_ContainerFormatPng,nullptr,&encoder)) && SUCCEEDED(encoder->Initialize(stream,WICBitmapEncoderNoCache)) &&
       SUCCEEDED(encoder->CreateNewFrame(&frame,nullptr)) && SUCCEEDED(frame->Initialize(nullptr)) && SUCCEEDED(frame->SetSize(UINT(w),UINT(h))) &&
       SUCCEEDED(frame->SetPixelFormat(&format)) && IsEqualGUID(format,GUID_WICPixelFormat32bppBGRA) &&
       SUCCEEDED(frame->WritePixels(UINT(h),UINT(w*4),UINT(w*h*4),reinterpret_cast<BYTE*>(bits))) && SUCCEEDED(frame->Commit()) && SUCCEEDED(encoder->Commit());
    if(frame) frame->Release(); if(encoder) encoder->Release(); if(stream) stream->Release();
    return ok;
}
// Draws a background image (any WIC format) into dc at its own size; false if it cannot be read.
bool DrawBackground(IWICImagingFactory* wic,HDC dc,const wchar_t* path,int& w,int& h,uint32_t*& bits) {
    IWICBitmapDecoder* decoder=nullptr; IWICBitmapFrameDecode* frame=nullptr; IWICFormatConverter* converter=nullptr; UINT bw=0,bh=0;
    bool ok=SUCCEEDED(wic->CreateDecoderFromFilename(path,nullptr,GENERIC_READ,WICDecodeMetadataCacheOnDemand,&decoder)) && SUCCEEDED(decoder->GetFrame(0,&frame)) &&
            SUCCEEDED(frame->GetSize(&bw,&bh)) && SUCCEEDED(wic->CreateFormatConverter(&converter)) &&
            SUCCEEDED(converter->Initialize(frame,GUID_WICPixelFormat32bppBGR,WICBitmapDitherTypeNone,nullptr,0,WICBitmapPaletteTypeCustom));
    if(ok) {
        BITMAPINFO bi={}; bi.bmiHeader.biSize=sizeof(bi.bmiHeader); bi.bmiHeader.biWidth=INT(bw); bi.bmiHeader.biHeight=-INT(bh); bi.bmiHeader.biPlanes=1; bi.bmiHeader.biBitCount=32;
        void* p=nullptr; HBITMAP dib=CreateDIBSection(dc,&bi,DIB_RGB_COLORS,&p,nullptr,0);
        ok=dib && p && SUCCEEDED(converter->CopyPixels(nullptr,bw*4,bw*bh*4,static_cast<BYTE*>(p)));
        if(ok) { SelectObject(dc,dib); w=INT(bw); h=INT(bh); bits=static_cast<uint32_t*>(p); } else if(dib) DeleteObject(dib);
    }
    if(converter) converter->Release(); if(frame) frame->Release(); if(decoder) decoder->Release();
    return ok;
}
}
bool RenderPreview(const vote::View& v,RECT block,bool controller,int hover_row,bool fetch_thumbs,const wchar_t* background,const wchar_t* path) noexcept {
    if(!v.ballot.count || !path || block.right<=block.left || block.bottom<=block.top) return false;
    const HRESULT com=CoInitializeEx(nullptr,COINIT_MULTITHREADED);
    IWICImagingFactory* wic=nullptr;
    if(FAILED(CoCreateInstance(CLSID_WICImagingFactory,nullptr,CLSCTX_INPROC_SERVER,IID_PPV_ARGS(&wic)))) { if(SUCCEEDED(com)) CoUninitialize(); return false; }
    if(fetch_thumbs) FetchNow(wic,v);
    pad_seen=controller; hover=hover_row;
    // The block is the lobby placement; the floating fallback is as wide, as tall as its choices.
    // Pixels per canvas unit, from the block's height; placed as in game.
    const double k=double(block.bottom-block.top)/double(kLobbyBlock.bottom-kLobbyBlock.top), unit=k*kUnitPerCanvas;
    HDC screen=GetDC(nullptr); HDC mem=CreateCompatibleDC(screen); ReleaseDC(nullptr,screen);
    int w=0,h=0; uint32_t* bits=nullptr;
    const bool floating=!background;
    RECT target=floating ? RECT{block.left,block.top,LONG(block.left+(kPanelRight-kLobbyBlock.left)*k+0.5),LONG(block.top+FloatingUnits(v.ballot.count)*unit+0.5)}
                         : LobbyPanel(block,k,v.ballot.count);
    if(!background || !DrawBackground(wic,mem,background,w,h,bits)) {
        w=target.right-target.left; h=target.bottom-target.top; target={0,0,w,h};
        BITMAPINFO bi={}; bi.bmiHeader.biSize=sizeof(bi.bmiHeader); bi.bmiHeader.biWidth=w; bi.bmiHeader.biHeight=-h; bi.bmiHeader.biPlanes=1; bi.bmiHeader.biBitCount=32;
        void* p=nullptr; HBITMAP dib=CreateDIBSection(mem,&bi,DIB_RGB_COLORS,&p,nullptr,0); if(dib && p) { SelectObject(mem,dib); bits=static_cast<uint32_t*>(p); }
    }
    bool ok=false;
    if(bits) {
        const int bw=target.right-target.left,bh=target.bottom-target.top;
        HDC panel=CreateCompatibleDC(mem); HBITMAP pb=CreateCompatibleBitmap(mem,bw,bh); HGDIOBJ old=SelectObject(panel,pb); SetBkMode(panel,TRANSPARENT);
        PaintHalo(panel,v,bw,bh,unit,!floating);
        BitBlt(mem,target.left,target.top,bw,bh,panel,0,0,SRCCOPY);
        SelectObject(panel,old); DeleteObject(pb); DeleteDC(panel); GdiFlush();
        ok=SavePng(wic,bits,w,h,path);
    }
    DeleteDC(mem); wic->Release(); if(SUCCEEDED(com)) CoUninitialize();
    return ok;
}
void SetEnabled(bool on) noexcept { enabled.store(on); Wake(); }
void Hold(bool on) noexcept { held.store(on); Wake(); }
void SetVoteKeys(const hotkey::Key (&keys)[hotkey::kVoteKeys]) noexcept { for(size_t i=0;i<hotkey::kVoteKeys;++i) vote_keys[i]=keys[i]; vote_keys_set=true; }
void SetLog(void (*sink)(const char*)) noexcept { log_sink.store(sink); }
bool LobbyShowing() noexcept { return lobby_block.load(); }
void SetLobbyBlock(bool on,float x0,float y0,float x1,float y1,float button_bottom) noexcept {
    AcquireSRWLockExclusive(&lobby_lock);
    lobby_rect[0]=x0; lobby_rect[1]=y0; lobby_rect[2]=x1; lobby_rect[3]=y1; lobby_rect[4]=button_bottom;
    const bool was=lobby_block.exchange(on); if(on && !was) lobby_log=true; // log the next placement
    ReleaseSRWLockExclusive(&lobby_lock);
    if(was!=on) Wake();
}
void Wake() noexcept {
    if(const HWND h=window.load()) { PostMessageW(h,kWake,0,0); return; }
    if(!enabled.load()) return;
    bool expected=false;
    if(!thread_started.compare_exchange_strong(expected,true)) return;
    HANDLE t=CreateThread(nullptr,0,Thread,nullptr,0,nullptr);
    if(t) CloseHandle(t); else thread_started.store(false);
}
}
}
