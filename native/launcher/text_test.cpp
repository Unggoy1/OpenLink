// String-ID hash tests against game IDs observed in the game (murmur3_32 of normalized text).
#include "text_hook.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
using namespace clientjoin::text;
void check(bool ok,const char* why) { if(!ok) { std::fprintf(stderr,"FAIL: %s\n",why); std::exit(1); } }
uint32_t Id(const char* s) { return StringId(s,std::strlen(s)); }
int main() {
    check(Id("custom_games_browser")==0x2cc43da2u,"custom_games_browser");
    check(Id("custom_games_landing")==0x7cf86ad7u,"custom_games_landing");
    check(Id("custom_game")==0x8f98c060u,"custom_game");
    check(Id("Custom Games-Browser")==0x2cc43da2u,"normalization: case, space and dash");
    const uint32_t id=Set("openlink/item/title","OpenLink Servers");
    check(id==Id("openlink/item/title") && id,"set returns the key's id");
    check(!Set("","x") && !Set(nullptr,"x") && !Set("k",nullptr),"invalid keys refused");
    check(Set("openlink/item/title","Renamed")==id,"replacing text keeps the id");
    check(SetOverlay(0x773922d8u,"OpenLink Servers") && !SetOverlay(0,"x") && !SetOverlay(1,nullptr),"overlay entries");
    std::puts("text tests passed");
}
