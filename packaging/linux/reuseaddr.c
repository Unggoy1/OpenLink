/* Preloaded into Wine in the container. The game server binds its LAN
 * discovery socket (UDP 7117) without SO_REUSEADDR. Windows still lets
 * OpenLink Server bind a second, SO_REUSEADDR socket there to read the
 * server's beacons; Linux (and Wine on Linux) needs the option on both
 * sockets. Wine's wineserver makes the bind() calls, so this sets
 * SO_REUSEADDR on every UDP socket bound to port 7117 just before its bind.
 * Other ports and TCP are untouched. */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <netinet/in.h>
#include <sys/socket.h>

#define DISCOVERY_PORT 7117

typedef int (*bind_fn)(int, const struct sockaddr *, socklen_t);

int bind(int fd, const struct sockaddr *addr, socklen_t len)
{
    static bind_fn real_bind;
    if (!real_bind)
        real_bind = (bind_fn)dlsym(RTLD_NEXT, "bind");
    if (addr) {
        int port = -1;
        if (addr->sa_family == AF_INET && len >= sizeof(struct sockaddr_in))
            port = ntohs(((const struct sockaddr_in *)addr)->sin_port);
        else if (addr->sa_family == AF_INET6 && len >= sizeof(struct sockaddr_in6))
            port = ntohs(((const struct sockaddr_in6 *)addr)->sin6_port);
        int type = 0;
        socklen_t size = sizeof type;
        if (port == DISCOVERY_PORT && getsockopt(fd, SOL_SOCKET, SO_TYPE, &type, &size) == 0 && type == SOCK_DGRAM) {
            int on = 1;
            setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &on, sizeof on);
        }
    }
    return real_bind(fd, addr, len);
}
