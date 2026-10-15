// Relay helper tests (no network).
#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include "relay.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
using namespace clientjoin::relay;
void check(bool ok,const char* why) { if(!ok) { std::fprintf(stderr,"FAIL: %s\n",why); std::exit(1); } }
int main() {
    uint8_t out[64]; const char* b64="AAECAwQFBgcICQoLDA0ODxAR";
    check(Base64Decode(b64,std::strlen(b64),out,sizeof(out))==18 && out[0]==0 && out[17]==17,"base64 decodes");
    check(Base64Decode("QQ==",4,out,sizeof(out))==1 && out[0]=='A',"base64 padding");
    check(!Base64Decode("QQ=",3,out,sizeof(out)) && !Base64Decode("Q===",4,out,sizeof(out)) && !Base64Decode("QQ=A",4,out,sizeof(out)),"bad base64 refused");
    check(!Base64Decode(b64,std::strlen(b64),out,4),"base64 respects capacity");
    const char list[]=R"([{"id":"aaa","name":"x \"q\" y","host":"127.0.0.1","port":1343,"match":{"phase":"lobby","id":"inner"},"tags":["id"]},)"
                      R"({"id":"5ee3ac6cc6209945","name":"Example","host":"server.example.net","port":62294,"proxy":true}])";
    char host[256]={}; uint16_t port=0;
    check(FindListing(list,std::strlen(list),"5ee3ac6cc6209945",host,sizeof(host),port) && !std::strcmp(host,"server.example.net") && port==62294,"listing by id");
    check(!FindListing(list,std::strlen(list),"inner",host,sizeof(host),port),"nested ids are not listings");
    check(!FindListing(list,std::strlen(list),"missing",host,sizeof(host),port),"missing id");
    check(!FindListing("[{\"id\":\"a\",\"host\":\"h\",\"port\":0}]",34,"a",host,sizeof(host),port),"zero port refused");
    check(!FindListing("[{\"id\":\"a\"",10,"a",host,sizeof(host),port),"truncated json refused");
    char id[40]={};
    check(FindListingByEndpoint(list,std::strlen(list),"127.0.0.1",1343,id,sizeof(id)) && !std::strcmp(id,"aaa"),"relocate by endpoint");
    check(!FindListingByEndpoint(list,std::strlen(list),"127.0.0.1",1344,id,sizeof(id)),"endpoint must match port");
    const char beacon[]=R"({"beacon":"AAECAwQFBgcICQoLDA0ODxAR","age_ms":1450})";
    uint8_t b[512]; size_t n=0; int64_t age=0;
    check(ParseBeacon(beacon,std::strlen(beacon),b,sizeof(b),n,age) && n==18 && age==1450,"beacon response");
    const char short_beacon[]=R"({"beacon":"QUFB","age_ms":1})";
    check(!ParseBeacon(short_beacon,std::strlen(short_beacon),b,sizeof(b),n,age),"beacon shorter than envelope refused");
    check(!ParseBeacon("{\"age_ms\":1}",12,b,sizeof(b),n,age),"beacon field required");
    check(IsBallot(reinterpret_cast<const uint8_t*>("HICOMM-BALLOT {}"),16) && !IsBallot(reinterpret_cast<const uint8_t*>("HICOMM-BALLO"),12),"ballot prefix");
    Config c={}; strcpy_s(c.directory,"http://127.0.0.1:8090"); strcpy_s(c.listing,"5ee3ac6cc6209945");
    check(ValidConfig(c),"loopback config");
    strcpy_s(c.lan_ip,"192.168.1.50"); check(ValidConfig(c),"broadcast config");
    strcpy_s(c.lan_ip,"127.0.0.1"); check(!ValidConfig(c),"loopback is not a LAN address");
    strcpy_s(c.lan_ip,"255.255.255.255"); check(!ValidConfig(c),"broadcast address refused");
    c.lan_ip[0]=0; strcpy_s(c.directory,"https://openlink-dir.example/v1"); check(!ValidConfig(c),"directory must not carry a path");
    strcpy_s(c.directory,"ftp://x"); check(!ValidConfig(c),"http(s) only");
    strcpy_s(c.directory,"https://dir.example"); strcpy_s(c.listing,"../x"); check(!ValidConfig(c),"listing id charset");
    {
        const char servers[]=R"([{"id":"a8ef2282e437c32e","name":"Example server","host":"x","port":1,"status":"ready","players":3,"joinable":true,"app_protocol":2,"description":"BTB \u003c3 caf\u00e9 \u20ac",)"
                             R"("match":{"phase":"lobby","entry":"e","name":"Fiesta Slayer on Interference","thumb":"t","mode":"m"}},)"
                             R"({"id":"b2","name":"no match","players":0,"joinable":false},{"name":"missing id"}])";
        Row rows[4]; size_t count=0;
        check(ParseServerList(servers,std::strlen(servers),rows,4,count) && count==2,"server rows parsed, id-less skipped");
        check(!std::strcmp(rows[0].id,"a8ef2282e437c32e") && !std::strcmp(rows[0].name,"Example server") && rows[0].players==3 && rows[0].joinable &&
              !std::strcmp(rows[0].match,"Fiesta Slayer on Interference") && !std::strcmp(rows[0].phase,"lobby"),"row fields");
        check(!std::strcmp(rows[1].id,"b2") && !rows[1].joinable && !rows[1].match[0] && rows[1].players==0,"row without match");
        check(!std::strcmp(rows[0].status,"ready") && !rows[1].status[0],"row status");
        check(!std::strcmp(rows[0].thumb,"t") && !std::strcmp(rows[0].mode,"m"),"row match thumb and mode");
        check(!std::strcmp(rows[0].description,"BTB <3 caf\xC3\xA9 \xE2\x82\xAC") && !rows[1].description[0],"row description with unicode escapes");
        check(rows[0].app_protocol==2 && !rows[1].app_protocol,"row app protocol (0 when not reported)");
        check(ParseServerList(servers,std::strlen(servers),rows,1,count) && count==1,"capacity bounds rows");
        check(!ParseServerList("[{\"id\":\"a\"",10,rows,4,count),"truncated list refused");
    }
    std::puts("relay tests passed");
}
