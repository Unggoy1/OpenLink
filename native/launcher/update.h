#pragma once
// Launcher update check, the same rules as internal/release (OpenLink Server's own check): the project's
// GitHub releases share one vMAJOR.MINOR.PATCH series but carry only the programs that changed,
// so the launcher looks for the newest release that contains its own file.
#include <cstddef>
namespace clientjoin {
namespace update {
// The releases list (newest first) and the prefix every release link must have.
constexpr char kListHost[]="api.github.com";
constexpr char kListPath[]="/repos/Unggoy1/OpenLink/releases?per_page=50";
constexpr char kPages[]="https://github.com/Unggoy1/OpenLink/releases/";
// The launcher's release file, as releases will carry it.
constexpr char kAsset[]="OpenLink-Launcher-windows-amd64.zip";
struct Found { char latest[64]; char url[256]; };
// True for a version that can get a notice: vMAJOR.MINOR.PATCH[-PRE]. A git describe build
// ("v0.7.6-7-gcc5dd6a", "-dirty") or "dev" is a development build and never does.
bool Released(const char* version) noexcept;
// Picks, from a GitHub releases JSON array, the newest release newer than current that contains
// asset; pre-releases only when current is one, never drafts, links only under kPages.
bool Pick(const char* json,size_t length,const char* current,const char* asset,Found& out) noexcept;
}
}
