#pragma once
// OpenLink Launcher version, set by build.cmd from `git describe` (e.g. v0.3.0); "dev" otherwise.
#ifndef OPENLINK_VERSION
#define OPENLINK_VERSION "dev"
#endif
#define OPENLINK_WIDEN2(x) L##x
#define OPENLINK_WIDEN(x) OPENLINK_WIDEN2(x)
#define OPENLINK_VERSION_W OPENLINK_WIDEN(OPENLINK_VERSION)
