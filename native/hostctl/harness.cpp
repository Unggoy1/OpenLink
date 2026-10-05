#define WIN32_LEAN_AND_MEAN
#include "bridge_api.h"
#include <cstdlib>
#include <cstdio>
#include <cstring>

int nibble(char c) { if(c>='0' && c<='9') return c-'0'; if(c>='a' && c<='f') return c-'a'+10; return -1; }
int main(int argc,char** argv) {
    if(argc==2 && std::strcmp(argv[1],"--loader-target")==0) {while(std::getchar()!=EOF){} return 0;}
    if(argc!=3) return 1;
    HostControlConfig config={}; config.size=sizeof(config); config.version=1;
    char backend[2]; if(GetEnvironmentVariableA("HICT_TEST_NATIVE",backend,sizeof(backend))==1 && backend[0]=='2') config.version=2;
    unsigned long port=std::strtoul(argv[2],nullptr,10); if(port==0 || port>65535) return 1; config.port=uint16_t(port);
    char token[65],pid[16];
    if(GetEnvironmentVariableA("HICT_TEST_TOKEN",token,sizeof(token))!=64 || GetEnvironmentVariableA("HICT_TEST_CONTROLLER_PID",pid,sizeof(pid))==0) return 1;
    config.controller_pid=uint32_t(std::strtoul(pid,nullptr,10));
    for(unsigned i=0;i<32;++i) { int a=nibble(token[2*i]),b=nibble(token[2*i+1]); if(a<0 || b<0) return 1; config.token[i]=uint8_t(16*a+b); }
    HMODULE dll=LoadLibraryA(argv[1]); if(!dll) { std::fprintf(stderr,"LoadLibrary error %lu\n",GetLastError()); return 2; }
    auto start=reinterpret_cast<HostControlStart>(GetProcAddress(dll,"HiHostControlStart"));
    auto wait=reinterpret_cast<HostControlWait>(GetProcAddress(dll,"HiHostControlWait"));
    auto stop=reinterpret_cast<HostControlStop>(GetProcAddress(dll,"HiHostControlStop"));
    if(!start || !wait || !stop) return 3;
    DWORD result=start(&config); if(result!=0) { std::fprintf(stderr,"Start error %lu\n",result); return 4; }
    char stop_mode[2];
    if(GetEnvironmentVariableA("HICT_TEST_STOP",stop_mode,sizeof(stop_mode))==1 && (stop_mode[0]=='1' || stop_mode[0]=='2')) {
        if(stop_mode[0]=='1' && std::getchar()==EOF) return 6;
        result=stop();
        if(result!=0) { std::fprintf(stderr,"Stop error %lu\n",result); return 7; }
        return 0;
    }
    result=wait(15000); if(result!=0) { stop(); std::fprintf(stderr,"Worker error %lu\n",result); return 5; }
    return 0;
}
