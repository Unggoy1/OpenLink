// OpenLink Launcher (OpenLink, AGPL-3.0). Finds Halo Infinite in the Steam libraries and
// starts it the same way as a direct shortcut to game\HaloInfinite.exe (working directory =
// install root), which runs without anti-cheat; or uses such a client that is already running.
// It then loads openlink-launcher.dll into that player client and calls HiOpenLinkStart.
// It never touches anti-cheat files, services or settings.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <tlhelp32.h>
#include <shlobj.h>
#include <shobjidl.h>
#include "boundary.h"
#include "version.h"
#include "config.h"
#include "netcheck.h"
#include "hotkey.h"
#include "update.h"
#include <winhttp.h>
#include <shellapi.h>
#include <cstdio>
#include <cstring>
#include <cwchar>
#include <string>
#include <vector>
namespace {
struct Handle {
    HANDLE value=nullptr;
    Handle()=default; explicit Handle(HANDLE h):value(h){}
    ~Handle(){ if(value && value!=INVALID_HANDLE_VALUE) CloseHandle(value); }
    Handle(const Handle&)=delete; Handle& operator=(const Handle&)=delete;
};
bool SameFile(const wchar_t* a,const wchar_t* b) {
    Handle x(CreateFileW(a,FILE_READ_ATTRIBUTES,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,nullptr,OPEN_EXISTING,0,nullptr));
    Handle y(CreateFileW(b,FILE_READ_ATTRIBUTES,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,nullptr,OPEN_EXISTING,0,nullptr));
    BY_HANDLE_FILE_INFORMATION i={},j={};
    return GetFileInformationByHandle(x.value,&i) && GetFileInformationByHandle(y.value,&j) &&
        i.dwVolumeSerialNumber==j.dwVolumeSerialNumber && i.nFileIndexHigh==j.nFileIndexHigh && i.nFileIndexLow==j.nFileIndexLow;
}
DWORD Module(DWORD pid,const wchar_t* path,uintptr_t* base,DWORD* size) {
    HANDLE snapshot=INVALID_HANDLE_VALUE;
    for(unsigned i=0;i<100;++i) { snapshot=CreateToolhelp32Snapshot(TH32CS_SNAPMODULE,pid); if(snapshot!=INVALID_HANDLE_VALUE || GetLastError()!=ERROR_BAD_LENGTH) break; Sleep(20); }
    if(snapshot==INVALID_HANDLE_VALUE) return GetLastError();
    Handle owner(snapshot); MODULEENTRY32W m={}; m.dwSize=sizeof(m);
    if(!Module32FirstW(snapshot,&m)) return GetLastError();
    do { if(SameFile(path,m.szExePath)) { *base=reinterpret_cast<uintptr_t>(m.modBaseAddr); *size=m.modBaseSize; return 0; } } while(Module32NextW(snapshot,&m));
    const DWORD e=GetLastError(); return e==ERROR_NO_MORE_FILES ? ERROR_MOD_NOT_FOUND : e;
}
DWORD Call(HANDLE process,uintptr_t entry,const void* data,size_t bytes,DWORD* result) {
    void* parameter=nullptr;
    if(bytes) {
        parameter=VirtualAllocEx(process,nullptr,bytes,MEM_COMMIT|MEM_RESERVE,PAGE_READWRITE); if(!parameter) return GetLastError();
        SIZE_T written=0; if(!WriteProcessMemory(process,parameter,data,bytes,&written) || written!=bytes) { const DWORD e=GetLastError(); VirtualFreeEx(process,parameter,0,MEM_RELEASE); return e ? e : ERROR_WRITE_FAULT; }
    }
    Handle thread(CreateRemoteThread(process,nullptr,0,reinterpret_cast<LPTHREAD_START_ROUTINE>(entry),parameter,0,nullptr));
    if(!thread.value) { const DWORD e=GetLastError(); if(parameter) VirtualFreeEx(process,parameter,0,MEM_RELEASE); return e; }
    const DWORD wait=WaitForSingleObject(thread.value,30000);
    if(wait!=WAIT_OBJECT_0) { std::fprintf(stderr,"remote call unfinished; argument retained\n"); return wait==WAIT_TIMEOUT ? ERROR_TIMEOUT : GetLastError(); }
    DWORD e=GetExitCodeThread(thread.value,result) ? 0 : GetLastError();
    if(parameter && !VirtualFreeEx(process,parameter,0,MEM_RELEASE) && !e) e=GetLastError();
    return e;
}
// Loads dll into the target and calls its HiOpenLinkStart export; returns its result.
DWORD Start(HANDLE process,DWORD pid,const wchar_t* dll) {
    HMODULE local=LoadLibraryExW(dll,nullptr,DONT_RESOLVE_DLL_REFERENCES); if(!local) return GetLastError();
    const auto* dos=reinterpret_cast<const IMAGE_DOS_HEADER*>(local);
    const auto* nt=reinterpret_cast<const IMAGE_NT_HEADERS*>(reinterpret_cast<uintptr_t>(local)+dos->e_lfanew);
    FARPROC entry=GetProcAddress(local,"HiOpenLinkStart");
    const uintptr_t rva=reinterpret_cast<uintptr_t>(entry)-reinterpret_cast<uintptr_t>(local);
    const DWORD image_size=nt->OptionalHeader.SizeOfImage;
    const bool valid=nt->FileHeader.Machine==IMAGE_FILE_MACHINE_AMD64 && entry && rva<image_size;
    FreeLibrary(local); if(!valid) return ERROR_BAD_EXE_FORMAT;
    uintptr_t remote=0; DWORD remote_size=0;
    DWORD e=Module(pid,dll,&remote,&remote_size);
    if(!e) return ERROR_ALREADY_EXISTS; // already loaded in this game process
    if(e!=ERROR_MOD_NOT_FOUND) return e;
    FARPROC load=GetProcAddress(GetModuleHandleW(L"kernel32.dll"),"LoadLibraryW"); HMODULE kernel=nullptr;
    if(!load || !GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,reinterpret_cast<LPCWSTR>(load),&kernel)) return GetLastError();
    wchar_t kernel_path[MAX_PATH]; const DWORD n=GetModuleFileNameW(kernel,kernel_path,MAX_PATH); if(!n || n>=MAX_PATH) return ERROR_INSUFFICIENT_BUFFER;
    uintptr_t kernel_base=0; DWORD kernel_size=0; e=Module(pid,kernel_path,&kernel_base,&kernel_size); if(e) return e;
    const uintptr_t load_rva=reinterpret_cast<uintptr_t>(load)-reinterpret_cast<uintptr_t>(kernel); if(load_rva>=kernel_size) return ERROR_INVALID_ADDRESS;
    DWORD result=0;
    e=Call(process,kernel_base+load_rva,dll,(std::wcslen(dll)+1)*sizeof(wchar_t),&result); if(e) return e;
    e=Module(pid,dll,&remote,&remote_size); if(e) return e;
    if(remote_size!=image_size) return ERROR_BAD_EXE_FORMAT;
    e=Call(process,remote+rva,nullptr,0,&result);
    return e ? e : result;
}
struct WindowSearch { DWORD pid; bool found; };
BOOL CALLBACK FindWindowOf(HWND w,LPARAM p) {
    auto* s=reinterpret_cast<WindowSearch*>(p); DWORD pid=0; GetWindowThreadProcessId(w,&pid);
    if(pid==s->pid && IsWindowVisible(w) && !GetWindow(w,GW_OWNER)) { s->found=true; return FALSE; }
    return TRUE;
}
bool HasWindow(DWORD pid) { WindowSearch s={pid,false}; EnumWindows(FindWindowOf,reinterpret_cast<LPARAM>(&s)); return s.found; }
// Processes running the same game file. OpenLink Server's own game server uses it too, so each
// one is classified once by CheckTarget (exact build, no anti-cheat, client or server); known
// servers are skipped afterwards. The full check hashes the game file, so the wait loop only
// runs it for a process that shows a window.
std::vector<DWORD> known_servers;
bool KnownServer(DWORD pid) { for(DWORD s:known_servers) if(s==pid) return true; return false; }
enum class Copy { Other,Client,Server,Unknown };
Copy Classify(const wchar_t* game,DWORD pid,bool full) {
    if(KnownServer(pid)) return Copy::Server;
    Handle q(OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION,FALSE,pid));
    if(!q.value) return GetLastError()==ERROR_ACCESS_DENIED ? Copy::Unknown : Copy::Other; // e.g. running as administrator
    wchar_t path[MAX_PATH*4]; DWORD length=MAX_PATH*4;
    if(!QueryFullProcessImageNameW(q.value,0,path,&length) || !SameFile(path,game)) return Copy::Other;
    if(!full) return Copy::Unknown;
    Handle h(OpenProcess(PROCESS_QUERY_INFORMATION|PROCESS_VM_READ|SYNCHRONIZE,FALSE,pid));
    if(!h.value) return Copy::Unknown;
    const DWORD e=clientjoin::CheckTarget(h.value,game);
    if(e==ERROR_INVALID_FUNCTION) { known_servers.push_back(pid); return Copy::Server; }
    return e ? Copy::Unknown : Copy::Client; // Unknown: still starting, or not checkable yet
}
template<class F> void EachHalo(F f) {
    Handle snapshot(CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS,0)); if(snapshot.value==INVALID_HANDLE_VALUE) return;
    PROCESSENTRY32W p={}; p.dwSize=sizeof(p);
    if(Process32FirstW(snapshot.value,&p)) do { if(!_wcsicmp(p.szExeFile,L"HaloInfinite.exe")) f(p.th32ProcessID); } while(Process32NextW(snapshot.value,&p));
}
// Before starting the game: is a copy of it (not OpenLink Server's game server) already running,
// or a Halo Infinite this launcher cannot inspect? Then wait for it instead of starting another.
bool GameCopyRunning(const wchar_t* game) {
    bool running=false;
    EachHalo([&](DWORD pid) { const Copy c=Classify(game,pid,true); if(c==Copy::Client || c==Copy::Unknown) running=true; });
    return running;
}
// The single ready player client (a visible window and a passing CheckTarget); only `only` if set.
DWORD FindClient(const wchar_t* game,DWORD& pid,DWORD only=0) {
    pid=0; unsigned clients=0;
    EachHalo([&](DWORD p) {
        if((only && p!=only) || !HasWindow(p)) return;
        if(Classify(game,p,true)==Copy::Client) { pid=p; ++clients; }
    });
    return clients==1 ? ERROR_SUCCESS : clients ? ERROR_TOO_MANY_SESS : ERROR_NOT_FOUND;
}
std::wstring Directory(const std::wstring& path) { const size_t at=path.find_last_of(L"\\/"); return at==std::wstring::npos ? L"." : path.substr(0,at); }
// Finds the supported game in the Steam libraries (registry Steam path, then every "path"
// in steamapps\libraryfolders.vdf). Returns an empty string when none passes ValidateFile.
std::wstring RegistryString(HKEY root,const wchar_t* key,const wchar_t* name) {
    wchar_t value[MAX_PATH]={}; DWORD bytes=sizeof(value);
    if(RegGetValueW(root,key,name,RRF_RT_REG_SZ,nullptr,value,&bytes)!=ERROR_SUCCESS) return L"";
    std::wstring s=value; for(auto& c:s) if(c==L'/') c=L'\\';
    return s;
}
std::wstring ReadUtf8(const std::wstring& path) {
    Handle f(CreateFileW(path.c_str(),GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,0,nullptr));
    if(f.value==INVALID_HANDLE_VALUE) return L"";
    LARGE_INTEGER size={}; if(!GetFileSizeEx(f.value,&size) || size.QuadPart<=0 || size.QuadPart>(1<<20)) return L"";
    std::string bytes(size_t(size.QuadPart),'\0'); DWORD read=0;
    if(!ReadFile(f.value,&bytes[0],DWORD(bytes.size()),&read,nullptr)) return L"";
    bytes.resize(read);
    const int n=MultiByteToWideChar(CP_UTF8,0,bytes.data(),int(bytes.size()),nullptr,0); if(n<=0) return L"";
    std::wstring text(size_t(n),L'\0'); MultiByteToWideChar(CP_UTF8,0,bytes.data(),int(bytes.size()),&text[0],n);
    return text;
}
std::wstring FindGame() {
    std::wstring steam=RegistryString(HKEY_CURRENT_USER,L"Software\\Valve\\Steam",L"SteamPath");
    if(steam.empty()) steam=RegistryString(HKEY_LOCAL_MACHINE,L"SOFTWARE\\WOW6432Node\\Valve\\Steam",L"InstallPath");
    if(steam.empty()) return L"";
    std::vector<std::wstring> libraries{steam};
    const std::wstring vdf=ReadUtf8(steam+L"\\steamapps\\libraryfolders.vdf");
    for(size_t at=vdf.find(L"\"path\""); at!=std::wstring::npos; at=vdf.find(L"\"path\"",at+6)) {
        const size_t open=vdf.find(L'"',at+6); if(open==std::wstring::npos) break;
        std::wstring path;
        size_t i=open+1;
        for(;i<vdf.size() && vdf[i]!=L'"';++i) { if(vdf[i]==L'\\' && i+1<vdf.size()) ++i; path+=vdf[i]; }
        if(!path.empty()) libraries.push_back(path);
        at=i;
    }
    for(const auto& library:libraries) {
        const std::wstring candidate=library+L"\\steamapps\\common\\Halo Infinite\\game\\HaloInfinite.exe";
        if(GetFileAttributesW(candidate.c_str())!=INVALID_FILE_ATTRIBUTES && clientjoin::ValidateFile(candidate.c_str())) return candidate;
    }
    return L"";
}
// Desktop shortcut "OpenLink" -> this launcher (working directory = its folder, game icon).
DWORD CreateShortcut(const std::wstring& self,const std::wstring& game,bool custom_game) {
    wchar_t desktop[MAX_PATH]; if(FAILED(SHGetFolderPathW(nullptr,CSIDL_DESKTOPDIRECTORY,nullptr,SHGFP_TYPE_CURRENT,desktop))) return ERROR_PATH_NOT_FOUND;
    const HRESULT init=CoInitializeEx(nullptr,COINIT_APARTMENTTHREADED);
    IShellLinkW* link=nullptr; IPersistFile* file=nullptr;
    HRESULT h=CoCreateInstance(CLSID_ShellLink,nullptr,CLSCTX_INPROC_SERVER,IID_IShellLinkW,reinterpret_cast<void**>(&link));
    if(SUCCEEDED(h)) {
        link->SetPath(self.c_str()); link->SetWorkingDirectory(Directory(self).c_str());
        link->SetDescription(L"Halo Infinite with OpenLink community servers");
        link->SetIconLocation(game.c_str(),0);
        if(custom_game) link->SetArguments((L"-game \""+game+L"\"").c_str());
        h=link->QueryInterface(IID_IPersistFile,reinterpret_cast<void**>(&file));
        if(SUCCEEDED(h)) { h=file->Save((std::wstring(desktop)+L"\\OpenLink.lnk").c_str(),TRUE); file->Release(); }
        link->Release();
    }
    if(SUCCEEDED(init)) CoUninitialize();
    return SUCCEEDED(h) ? ERROR_SUCCESS : DWORD(h);
}
bool ProcessRunning(const wchar_t* name) {
    Handle snapshot(CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS,0)); if(snapshot.value==INVALID_HANDLE_VALUE) return true; // unknown: do not block
    PROCESSENTRY32W p={}; p.dwSize=sizeof(p);
    if(Process32FirstW(snapshot.value,&p)) do { if(!_wcsicmp(p.szExeFile,name)) return true; } while(Process32NextW(snapshot.value,&p));
    return false;
}
// Paths may hold any characters; the console is switched to UTF-8 in wmain.
std::string Utf8(const std::wstring& text) {
    const int n=WideCharToMultiByte(CP_UTF8,0,text.c_str(),int(text.size()),nullptr,0,nullptr,nullptr); if(n<=0) return "";
    std::string out(size_t(n),'\0'); WideCharToMultiByte(CP_UTF8,0,text.c_str(),int(text.size()),&out[0],n,nullptr,nullptr); return out;
}
// Windows' own description of an error code, with the code (e.g. "Access is denied. (5)").
std::string ErrorText(DWORD code) {
    char text[256]={};
    DWORD n=FormatMessageA(FORMAT_MESSAGE_FROM_SYSTEM|FORMAT_MESSAGE_IGNORE_INSERTS,nullptr,code,0,text,sizeof(text),nullptr);
    while(n && (text[n-1]=='\r' || text[n-1]=='\n' || text[n-1]==' ' || text[n-1]=='.')) text[--n]=0;
    char out[300]; sprintf_s(out,"%s (%lu)",n ? text : "error",code); return out;
}
// GET https://<host><path> (body at most 4 MB, 5 s timeouts). False on any failure.
bool HttpsGet(const wchar_t* host,const wchar_t* path,std::string& body) {
    body.clear(); bool ok=false;
    // One literal: building a string from two uses of the version macro would span two separate copies.
    const wchar_t* const agent=L"OpenLink-Launcher/" OPENLINK_VERSION_W;
    HINTERNET session=WinHttpOpen(agent,WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,WINHTTP_NO_PROXY_NAME,WINHTTP_NO_PROXY_BYPASS,0);
    if(!session) return false;
    WinHttpSetTimeouts(session,5000,5000,5000,5000);
    HINTERNET connect=WinHttpConnect(session,host,INTERNET_DEFAULT_HTTPS_PORT,0);
    HINTERNET request=connect ? WinHttpOpenRequest(connect,L"GET",path,nullptr,WINHTTP_NO_REFERER,WINHTTP_DEFAULT_ACCEPT_TYPES,WINHTTP_FLAG_SECURE) : nullptr;
    if(request && WinHttpSendRequest(request,L"Accept: application/vnd.github+json\r\n",DWORD(-1),WINHTTP_NO_REQUEST_DATA,0,0,0) && WinHttpReceiveResponse(request,nullptr)) {
        DWORD status=0,size=sizeof(status);
        if(WinHttpQueryHeaders(request,WINHTTP_QUERY_STATUS_CODE|WINHTTP_QUERY_FLAG_NUMBER,WINHTTP_HEADER_NAME_BY_INDEX,&status,&size,WINHTTP_NO_HEADER_INDEX) && status==200) {
            ok=true; char buffer[16384];
            for(DWORD got=0;WinHttpReadData(request,buffer,sizeof(buffer),&got) && got;) {
                if(body.size()+got>(4u<<20)) { ok=false; break; }
                body.append(buffer,got);
            }
        }
    }
    if(request) WinHttpCloseHandle(request); if(connect) WinHttpCloseHandle(connect); WinHttpCloseHandle(session);
    return ok;
}
// Before the game starts: a newer launcher release asks whether to open its download page.
// True when the player chose to update now (the launcher then ends). Offline or any error: no notice.
bool OfferUpdate() {
    using namespace clientjoin::update;
    if(!Released(OPENLINK_VERSION)) return false; // development builds never ask
    std::wstring host(kListHost,kListHost+sizeof(kListHost)-1),path(kListPath,kListPath+sizeof(kListPath)-1);
    std::string body; Found found;
    if(!HttpsGet(host.c_str(),path.c_str(),body) || !Pick(body.data(),body.size(),OPENLINK_VERSION,kAsset,found)) return false;
    std::printf("A newer OpenLink Launcher is available: %s (you have %s).\n",found.latest,OPENLINK_VERSION);
    DWORD mode=0;
    if(!GetConsoleMode(GetStdHandle(STD_INPUT_HANDLE),&mode)) { std::printf("Download it from %s\n\n",found.url); return false; }
    std::printf("Open the download page now? [Y/n] "); std::fflush(stdout);
    char answer[16]={};
    if(!std::fgets(answer,sizeof(answer),stdin)) answer[0]=0;
    if(answer[0]=='n' || answer[0]=='N') { std::puts(""); return false; }
    const std::wstring url(found.url,found.url+std::strlen(found.url));
    ShellExecuteW(nullptr,L"open",url.c_str(),nullptr,nullptr,SW_SHOWNORMAL);
    std::printf("Opened %s\nStart OpenLink again after updating.\n",found.url);
    return true;
}
void Usage() {
    std::puts("Starts Halo Infinite without anti-cheat and adds the OpenLink Servers browser to\n"
              "Play > Custom Game. Run it with no options; settings are in openlink-launcher.json.\n\n"
              "  -create-shortcut  put an OpenLink shortcut on your Desktop\n"
              "  -where            show which Halo Infinite was found\n"
              "  -game <path>      use this game\\HaloInfinite.exe instead of the Steam one\n"
              "  -attach           wait for a Halo Infinite started another way\n"
              "  -version          show the version");
}
// When started by double-click the console is ours alone; keep errors readable.
bool OwnConsole() { DWORD ids[2]; return GetConsoleProcessList(ids,2)==1; }
int Finish(int code) {
    if(OwnConsole()) {
        if(code) { std::puts("Press Enter to close."); (void)std::getchar(); }
        else Sleep(4000);
    }
    return code;
}
}

int wmain(int argc,wchar_t** argv) {
    SetConsoleTitleW(L"OpenLink Launcher"); SetConsoleOutputCP(CP_UTF8);
    std::printf("OpenLink Launcher %s - Halo Infinite community servers\n\n",OPENLINK_VERSION);
    // No arguments needed: the game is found in the Steam libraries; an already running
    // anti-cheat-free player client is used as is, otherwise the game starts like the direct shortcut.
    bool attach=false,shortcut=false,where=false; std::wstring game;
    for(int i=1;i<argc;++i) {
        if(!_wcsicmp(argv[i],L"-attach")) attach=true;
        else if(!_wcsicmp(argv[i],L"-create-shortcut")) shortcut=true;
        else if(!_wcsicmp(argv[i],L"-where")) where=true;
        else if(!_wcsicmp(argv[i],L"-version")) return 0;
        else if(!_wcsicmp(argv[i],L"-game") && i+1<argc) game=argv[++i];
        else { Usage(); return Finish(_wcsicmp(argv[i],L"-help") && _wcsicmp(argv[i],L"/?") ? 2 : 0); }
    }
    const bool custom_game=!game.empty(); // -game given: the Desktop shortcut keeps it
    if(game.empty()) {
        game=FindGame();
        if(game.empty()) { std::puts("Halo Infinite was not found in your Steam libraries.\nRun: openlink-launcher -game \"<install folder>\\game\\HaloInfinite.exe\""); return Finish(1); }
    }
    wchar_t full[MAX_PATH]; if(!GetFullPathNameW(game.c_str(),MAX_PATH,full,nullptr) || !clientjoin::ValidateFile(full)) { std::printf("%s is not the supported Halo Infinite version.\n",Utf8(game).c_str()); return Finish(1); }
    game=full;
    if(where) { std::printf("Halo Infinite: %s\n",Utf8(game).c_str()); return Finish(0); }
    wchar_t self[MAX_PATH]; const DWORD n=GetModuleFileNameW(nullptr,self,MAX_PATH); if(!n || n>=MAX_PATH) return Finish(1);
    if(shortcut) {
        const DWORD s=CreateShortcut(self,game,custom_game);
        if(s) { std::printf("Could not create the Desktop shortcut (0x%08lx).\n",s); return Finish(1); }
        std::puts("Created the OpenLink shortcut on your Desktop. Use it to play with OpenLink.");
        return Finish(0);
    }
    const std::wstring dll=Directory(self)+L"\\openlink-launcher.dll";
    if(GetFileAttributesW(dll.c_str())==INVALID_FILE_ATTRIBUTES) { std::puts("openlink-launcher.dll is missing. Keep it in the same folder as openlink-launcher.exe."); return Finish(1); }
    {
        // The DLL refuses the same settings; saying why here, before the game starts, is clearer.
        char json[4096],directory[160];
        if(const DWORD c=clientjoin::config::Read(nullptr,json)) { std::printf("Could not read openlink-launcher.json: %s\n",ErrorText(c).c_str()); return Finish(1); }
        if(clientjoin::config::String(json,"directory",directory,sizeof(directory)) && directory[0] && !clientjoin::netcheck::DirectoryAllowed(directory)) {
            std::printf("openlink-launcher.json: %s.\n",clientjoin::netcheck::kDirectoryRule); return Finish(1);
        }
        char keys[160],why[128]; clientjoin::hotkey::Key parsed[clientjoin::hotkey::kVoteKeys];
        if(std::strstr(json,"\"vote_keys\"") && (!clientjoin::config::String(json,"vote_keys",keys,sizeof(keys)) || !clientjoin::hotkey::ParseVoteKeys(keys,parsed,why,sizeof(why)))) {
            std::printf("openlink-launcher.json: vote_keys: %s.\nWrite four different keys, e.g. \"Alt+1, Alt+2, Alt+3, Alt+4\".\n",
                        clientjoin::config::String(json,"vote_keys",keys,sizeof(keys)) ? why : "not a short text value");
            return Finish(1);
        }
    }
    // The update check must never keep anyone from playing: any failure means no notice.
    bool update_chosen=false;
    try { update_chosen=OfferUpdate(); } catch(...) { update_chosen=false; }
    if(update_chosen) return Finish(0);
    DWORD e=clientjoin::CheckNoEac(0);
    if(e) {
        std::printf("Easy Anti-Cheat is running or could not be checked (%s).\n"
                    "Close Halo Infinite if you started it from Steam (or another game that uses it), then start OpenLink again.\n",ErrorText(e).c_str());
        return Finish(1);
    }
    DWORD pid=0; Handle launched;
    if(!attach) {
        DWORD running=0;
        if(FindClient(game.c_str(),running)==ERROR_SUCCESS) { attach=true; std::printf("Halo Infinite is already running (pid %lu); adding OpenLink to it.\n",running); }
        else if(GameCopyRunning(game.c_str())) {
            // Still starting, or running as administrator: never start a second copy.
            attach=true;
            std::puts("Halo Infinite is already starting or running; waiting for it.\n"
                      "(If it runs as administrator, run OpenLink as administrator too.)");
        }
    }
    if(!attach) {
        if(!ProcessRunning(L"steam.exe")) { std::puts("Steam is not running. Start Steam and sign in, then start OpenLink again."); return Finish(1); }
        // Same as the direct shortcut: game\HaloInfinite.exe with the install root as working directory.
        std::printf("Starting %s\n",Utf8(game).c_str());
        const std::wstring root=Directory(Directory(game)); std::wstring command=L"\""+game+L"\"";
        STARTUPINFOW si={}; si.cb=sizeof(si); PROCESS_INFORMATION pi={};
        if(!CreateProcessW(game.c_str(),&command[0],nullptr,nullptr,FALSE,0,nullptr,root.c_str(),&si,&pi)) { const DWORD c=GetLastError(); std::printf("Could not start Halo Infinite: %s\n",ErrorText(c).c_str()); return Finish(1); }
        CloseHandle(pi.hThread); launched.value=pi.hProcess; pid=pi.dwProcessId;
        std::puts("Waiting for the game window...");
    } else std::puts("Waiting for Halo Infinite...");
    // Wait (up to 10 minutes) for exactly one player client with a visible window (ours, if we
    // started it).
    const ULONGLONG until=GetTickCount64()+600000;
    for(;;) {
        DWORD found=0; e=FindClient(game.c_str(),found,pid);
        if(!e) { pid=found; break; }
        if(launched.value && WaitForSingleObject(launched.value,0)!=WAIT_TIMEOUT) { std::puts("Halo Infinite closed before OpenLink could start."); return Finish(1); }
        if(e==ERROR_TOO_MANY_SESS) { std::puts("More than one Halo Infinite is running. Close the extra copies, then start OpenLink again."); return Finish(1); }
        if(GetTickCount64()>=until) { std::puts("Halo Infinite did not open its window within 10 minutes."); return Finish(1); }
        Sleep(1000);
    }
    Sleep(5000); // let the window finish initializing; the DLL itself waits for the menu scripts
    Handle process(OpenProcess(PROCESS_QUERY_INFORMATION|PROCESS_VM_READ|PROCESS_VM_WRITE|PROCESS_VM_OPERATION|PROCESS_CREATE_THREAD|SYNCHRONIZE,FALSE,pid));
    if(!process.value) { const DWORD c=GetLastError(); std::printf("Could not open the game process: %s\n",ErrorText(c).c_str()); return Finish(1); }
    e=clientjoin::CheckTarget(process.value,game.c_str());
    if(e) { std::printf("This game process cannot use OpenLink (%s); nothing was loaded.\n",ErrorText(e).c_str()); return Finish(1); }
    e=Start(process.value,pid,dll.c_str());
    if(e==ERROR_ALREADY_EXISTS) { std::puts("OpenLink was already added to this game. To start it again, restart Halo Infinite."); return Finish(0); }
    if(e) { std::printf("OpenLink did not start: %s\nDetails are in openlink-launcher.log next to this program.\n",ErrorText(e).c_str()); return Finish(1); }
    std::puts("OpenLink is running. In Halo Infinite open Play > Custom Game > OpenLink Servers.");
    return Finish(0);
}
