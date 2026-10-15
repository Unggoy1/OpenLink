// Development tool: renders the vote panel for sample ballots into PNG files, without the game.
// usage: overlay-preview.exe <output-folder> [-thumbs] [-lobby <screenshot> left top right bottom]
//   -thumbs  downloads the real map art
//   -lobby   draws the panel into that rectangle of a lobby screenshot (the stock menu block);
//            without it the floating panel is drawn alone at 1440p size.
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "overlay.h"
#include <cstdio>
#include <cstring>
#include <cwchar>
#include <string>
using namespace clientjoin;
namespace {
void Option(relay::Ballot& b,const char* id,const char* name,const char* thumb,int votes) {
    relay::BallotOption& o=b.options[b.count]; strcpy_s(o.id,id); strcpy_s(o.name,name); strcpy_s(o.thumb,thumb);
    b.counts[b.count++]=votes;
}
constexpr char kInterference[]="70f884d7-6869-469d-b4d2-4219627e2d83/cc791b4b-054a-4653-9034-5dc13c809c54";
constexpr char kKusini[]="4eb7a3ac-81f7-4faa-acd8-ce6bbba667af/98a5391c-4a3a-4f04-bdc7-6db58cc27433";
}
int wmain(int argc,wchar_t** argv) {
    if(argc<2) { std::puts("usage: overlay-preview <output-folder> [-thumbs] [-lobby <screenshot> left top right bottom]"); return 2; }
    const std::wstring out=argv[1]; bool thumbs=false; const wchar_t* lobby=nullptr;
    // Floating: as wide as the lobby block at 1440p (531 canvas units x 1440/1080), its scale.
    RECT block={0,0,708,335};
    for(int i=2;i<argc;++i) {
        if(!_wcsicmp(argv[i],L"-thumbs")) thumbs=true;
        else if(!_wcsicmp(argv[i],L"-lobby") && i+5<argc) { lobby=argv[++i]; block={_wtoi(argv[i+1]),_wtoi(argv[i+2]),_wtoi(argv[i+3]),_wtoi(argv[i+4])}; i+=4; }
    }
    vote::View four={}; four.ballot.round=7; four.ballot.mine=1; four.ballot.winner=-1; four.ballot.start_ms=-1;
    Option(four.ballot,"interference-fiesta","Fiesta Slayer on Interference",kInterference,2);
    Option(four.ballot,"kusini-ctf","CTF: Arena on Kusini Bay",kKusini,1);
    Option(four.ballot,"interference-ctf","CTF: Arena on Interference",kInterference,0);
    Option(four.ballot,"kusini-slayer","Slayer on Kusini Bay",kKusini,1);
    four.remaining_s=17.4; four.starts_in_s=-1; four.mine=1;
    vote::View two=four; two.ballot.count=2; two.mine=0; two.ballot.counts[0]=1; two.ballot.counts[1]=0;
    vote::View closed=two; closed.ballot.closed=true; closed.ballot.winner=0; closed.remaining_s=0; closed.starts_in_s=3.2;
    struct { const vote::View* v; bool pad; int hover; const wchar_t* name; } shots[]={
        {&two,false,-1,L"open-2.png"},{&four,true,2,L"open-4.png"},{&closed,false,-1,L"closed-2.png"}};
    for(const auto& s:shots) {
        const std::wstring path=out+L"\\"+(lobby ? L"lobby-" : L"floating-")+s.name;
        const bool ok=overlay::RenderPreview(*s.v,block,s.pad,s.hover,thumbs,lobby,path.c_str());
        std::printf("%ls: %s\n",path.c_str(),ok ? "ok" : "FAILED");
        if(!ok) return 1;
    }
    return 0;
}
