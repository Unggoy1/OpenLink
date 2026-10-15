-- OpenLink in-game server browser (OpenLink, AGPL-3.0). openlink-launcher.dll runs this script in
-- Halo Infinite's front-end UI state and calls into it from the engine tick. It:
--  * adds "OpenLink Servers" to Play > Custom Game; it opens the stock Customs Browser in OpenLink
--    mode (the DLL feeds it directory servers), or a simple list page if those hooks are missing;
--  * hides the filters panel, filter button and empty-state shortcuts while in OpenLink mode;
--  * shows a stock OK dialog when a join is refused (OpenLinkUI.notify, text set by the DLL);
--  * after the player leaves an OpenLink match, replaces the stuck LAN Create Match screen with
--    the browser (lobbyPresent/openBrowserRoot), with Back + reopen as a fallback.
-- Rows and join methods are keyed by listing id; any error falls back to the stock menus.
if OpenLinkUI == nil and type(AddListItemsToCUI) == "function" then
  OpenLinkUI = { cb = 0, stockWrapped = false, landing = 0, clicks = 0, launches = 0, built = 0, inplace = 0, rows = 0, joins = 0, joinok = 0, err = "", browse = false }
  local original = AddListItemsToCUI
  OpenLinkUI.original = original
  local function log(s) if type(OpenLink) == "table" and OpenLink.Log then pcall(OpenLink.Log, tostring(s)) end end
  local function fail(where, e) OpenLinkUI.err = where .. ": " .. tostring(e); log(OpenLinkUI.err) end
  local function isLanding(items, typename)
    return typename == "PlayMenuMenuItem" and type(items) == "table" and type(items[2]) == "table"
      and items[2].CallbackFunction == "OpenCustomGameEditor" and type(CustomGamesLandingScreen) == "table"
  end
  -- Browser rows: Refresh first, then one row per listing; join methods are named after
  -- the listing id so a row's text (also keyed by id in the DLL) and its target agree.
  local function serverRows()
    local rows = {}
    rows[1] = { Title = "openlink/ui/refresh", Description = "openlink/ui/refresh_desc", CallbackFunction = "OpenLinkRefresh", IsEnabled = true }
    local n = (type(OpenLink) == "table" and OpenLink.Count()) or -1
    if n and n > 0 then
      for i = 1, n do
        local id, title, desc, joinable = OpenLink.Row(i)
        if id then
          local method = "OpenLinkJoin_" .. id
          CustomGamesLandingScreen[method] = function(self, sender, args)
            OpenLinkUI.joins = OpenLinkUI.joins + 1
            local ok, msg = OpenLink.Join(id)
            if ok then OpenLinkUI.joinok = OpenLinkUI.joinok + 1 end
            log("join " .. tostring(id) .. " -> " .. tostring(ok) .. " " .. tostring(msg))
          end
          rows[#rows + 1] = { Title = title, Description = desc, CallbackFunction = method, IsEnabled = joinable }
        end
      end
    end
    if #rows == 1 then
      rows[2] = { Title = (n == -1) and "openlink/ui/loading" or "openlink/ui/empty", Description = "openlink/ui/empty_desc", CallbackFunction = "OpenLinkRefresh", IsEnabled = true }
    end
    OpenLinkUI.rows = #rows
    log("browser rows " .. tostring(#rows) .. " (servers " .. tostring(n) .. ")")
    return rows
  end
  -- Fallback: replace the current page's list in place.
  local function rebuild(screen)
    local list = screen and screen.dc and screen.dc.MenuItems
    if not list then fail("rebuild", "no list"); return false end
    local ok, rows = pcall(serverRows)
    if not ok then fail("rows", rows); return false end
    local cleared, e = pcall(function() list:Clear() end)
    if not cleared then fail("clear", e); return false end
    local added, e2 = pcall(original, rows, list, "PlayMenuMenuItem")
    if not added then fail("add", e2); return false end
    OpenLinkUI.inplace = OpenLinkUI.inplace + 1
    return true
  end
  local function mode() return type(OpenLink) == "table" and OpenLink.BrowserMode ~= nil and OpenLink.BrowserMode() == true end
  -- Refused join: the DLL replaces this dialog's title/body text while its notice is fresh.
  OpenLinkUI.notify = function()
    local ci = 0
    pcall(function() ci = OpenLinkUI.screen.Screen.ControllerIndex or 0 end)
    local ok, e = pcall(SLAMS.OpenDialog, ci, "campaign_generic_dialog")
    if not ok then fail("notice", e) end
  end
  -- after an OpenLink match the game returns to CustomGameScreen (Create Match) as a LAN lobby
  -- whose IsLoadingData never clears (FN064). The DLL samples lobbyStuck, then calls leaveStuck
  -- (stock Back) and openAfterBack (our browser from the Custom Game landing).
  -- Logs, once per Lua state, what Lua can read on the post-match screen (diagnostics).
  local function describe(s)
    if OpenLinkUI.described then return end
    OpenLinkUI.described = true
    local function field(k)
      local ok, v = pcall(function() return s.dc[k] end)
      if ok then return tostring(v) end
      return "error " .. tostring(v)
    end
    local ok, a = pcall(function() return SLAMS.GetActiveScreen() end)
    log("post-match screen: object=" .. tostring(s ~= nil) .. " IsLANLobby=" .. field("IsLANLobby") .. " IsLoadingData=" .. field("IsLoadingData") .. " active=" .. tostring(ok and a or "error"))
  end
  OpenLinkUI.lobbyStuck = function()
    if type(CustomGameScreen) ~= "table" or type(GetObject) ~= "function" then return false end
    local s = GetObject(CustomGameScreen)
    if not s then return false end
    describe(s)
    local loading = true
    pcall(function() if s.dc.IsLoadingData == false or s.dc.IsLANLobby == true then loading = false end end)
    if loading and not OpenLinkUI.stuckLogged then OpenLinkUI.stuckLogged = true; log("Create Match screen still present after the match") end
    return loading
  end
  OpenLinkUI.leaveStuck = function()
    local s = GetObject(CustomGameScreen)
    if not s then return false end
    local ok, e = pcall(function() s:ContentGoBack() end)
    if not ok then fail("post-match back", e) end
    return ok
  end
  -- the Customs Browser has no registered parent, so Back from a root browser would do nothing.
  -- With Custom Game landing as its parent, Back from a browser opened as root goes there (FN065).
  pcall(RegisterSlamsParent, "custom_games_browser", "custom_games_landing")
  -- O178/O180: after leaving, Create Match reads IsLANLobby=false IsLoadingData=true; after a normal match
  -- end the player is still in the server lobby (IsLANLobby=true IsLoadingData=false) and must stay there.
  OpenLinkUI.lobbyPresent = function()
    if type(CustomGameScreen) ~= "table" or type(GetObject) ~= "function" then return false end
    local s = GetObject(CustomGameScreen)
    if not s then return false end
    describe(s)
    local left = false
    pcall(function() left = s.dc.IsLoadingData == true and s.dc.IsLANLobby ~= true end)
    return left
  end
  -- Replaces the post-match Create Match screen (and its back stack) with our browser in OpenLink mode.
  OpenLinkUI.openBrowserRoot = function()
    if type(OpenLink) ~= "table" or not OpenLink.SetBrowserMode or type(SLAMS) ~= "table" or type(SLAMS.OpenContentScreen) ~= "function" then return false end
    local ci = 0
    pcall(function() ci = GetObject(CustomGameScreen).Screen.ControllerIndex or 0 end)
    if not OpenLink.SetBrowserMode(true) then return false end
    local ok, r = pcall(SLAMS.OpenContentScreen, ci, "custom_games_browser", function(h) if h then SLAMS.SetActiveScreen(h) end end, false)
    log("post-match open browser: " .. tostring(ok) .. " " .. tostring(r))
    if not ok or r == 0 or r == false then pcall(OpenLink.SetBrowserMode, false); return false end
    OpenLinkUI.cb = OpenLinkUI.cb + 1
    pcall(OpenLinkUI.wrapDispose)
    return true
  end
  OpenLinkUI.openAfterBack = function()
    if type(CustomGamesLandingScreen) ~= "table" or type(GetObject) ~= "function" then return false end
    local s = GetObject(CustomGamesLandingScreen)
    if not s or type(s.OpenLinkServers) ~= "function" then return false end
    local ok, e = pcall(function() s:OpenLinkServers() end)
    if not ok then fail("post-match open", e) end
    return ok
  end
  -- Wraps one CustomGamesBrowserScreen method once (inherited ones too).
  local function wrap(cls, name, make)
    local flag = "__openlink" .. name
    if rawget(cls, flag) then return end
    local stock = cls[name]
    if type(stock) ~= "function" then return end
    rawset(cls, name, make(stock))
    rawset(cls, flag, true)
  end
  -- Climbs at most 32 parents from w; true when a is among them (w included).
  local function encloses(a, w)
    local n = 0
    while w ~= nil and n < 32 do
      if rawequal(w, a) or w == a then return true end
      w = w.Parent; n = n + 1
    end
    return false
  end
  -- OpenLink mode: no filters panel, filter button or empty-state shortcuts on this screen.
  local function hideStock(self)
    pcall(function() self.filterList.Visible = false end)
    -- The FILTERS header and scroll arrows sit in the list's parent; never hide a parent that
    -- also holds the grid (or the screen itself).
    pcall(function()
      local p = self.filterList.Parent
      if p ~= nil and not rawequal(p, self.Screen) and not encloses(p, self.gridlist) and p.Visible ~= false then
        p.Visible = false
        if not OpenLinkUI.parentHidden then OpenLinkUI.parentHidden = true; log("filters panel parent hidden") end
      end
    end)
    pcall(function() self.shortcutList.Visible = false end)
    pcall(function() HelpBarScreen.SetButtonVisible(self.Screen, k_buttons.aux2, false) end)
  end
  OpenLinkUI.hideStock = hideStock
  local function wrapBrowser(cls)
    wrap(cls, "ScreenInitialize", function(stock) return function(self, ...)
      local r = stock(self, ...)
      OpenLinkUI.screen = self
      if mode() then hideStock(self) end
      return r
    end end)
    -- Filters do not apply to OpenLink servers (and the shortcuts are hidden): focus the grid.
    wrap(cls, "FocusFilterList", function(stock) return function(self, ...)
      if not mode() then return stock(self, ...) end
      if self.FocusGridList then return self:FocusGridList() end
    end end)
    wrap(cls, "OnAux2Pressed", function(stock) return function(self, ...)
      if mode() then return end
      return stock(self, ...)
    end end)
    wrap(cls, "UpdateHelpbar", function(stock) return function(self, ...)
      local r = stock(self, ...)
      OpenLinkUI.screen = self
      -- Also covers a first open whose ScreenInitialize ran before these wraps existed.
      if mode() then
        hideStock(self)
      end
      return r
    end end)
  end
  -- Leaving the browser turns OpenLink mode off (wrapped when the classes exist).
  OpenLinkUI.wrapDispose = function()
    -- Only the browser class: other results screens (e.g. details) must not end the mode.
    local cls = CustomGamesBrowserScreen
    if type(cls) == "table" and not rawget(cls, "__openlinkDispose") then
      local disposeOriginal = cls.ScreenDispose
      rawset(cls, "ScreenDispose", function(self, ...)
        if type(OpenLink) == "table" and OpenLink.SetBrowserMode then pcall(OpenLink.SetBrowserMode, false) end
        if disposeOriginal then return disposeOriginal(self, ...) end
      end)
      rawset(cls, "__openlinkDispose", true)
      log("wrapped CustomGamesBrowserScreen.ScreenDispose")
    end
    if type(cls) == "table" then wrapBrowser(cls) end
    -- A browser opened before these wraps existed: fix the live screen too.
    if type(cls) == "table" and mode() and type(GetObject) == "function" then
      pcall(function() local s = GetObject(cls); if s then OpenLinkUI.screen = s; hideStock(s) end end)
    end
  end
  -- Vote panel placement: on the server lobby screen (CustomGameScreen, not loading) the DLL draws
  -- the vote over the lobby's greyed-out host rows; elsewhere it floats. The rows' rectangle comes
  -- from the widgets themselves: position + scale x size, up the parent chain (as the game's own
  -- LocalToGlobal does, FN068), in UI units. Logged once per screen.
  local function globalRect(w)
    local x0, y0, x1, y1 = 0, 0, w.Width, w.Height
    if type(x1) ~= "number" or type(y1) ~= "number" then return nil end
    local n = w
    for depth = 1, 32 do
      if n == nil then break end
      local px, py, sx, sy = nil, nil, nil, nil
      if not pcall(function() px, py, sx, sy = n.PositionX, n.PositionY, n.ScaleX, n.ScaleY end) then break end
      if type(px) ~= "number" or type(py) ~= "number" then break end
      if type(sx) ~= "number" or sx == 0 then sx = 1 end
      if type(sy) ~= "number" or sy == 0 then sy = 1 end
      x0, x1, y0, y1 = px + sx * x0, px + sx * x1, py + sy * y0, py + sy * y1
      local parent = nil
      if not pcall(function() parent = n.Parent end) then break end
      n = parent
    end
    return x0, y0, x1, y1
  end
  OpenLinkUI.voteTick = function()
    if type(OpenLink) ~= "table" or not OpenLink.SetLobbyVote then return end
    local on, x0, y0, x1, y1, bottom = false, 0, 0, 0, 0, 0
    if type(CustomGameScreen) == "table" and type(GetObject) == "function" then
      local s = nil
      pcall(function() s = GetObject(CustomGameScreen) end)
      if s then
        local loading, lan = nil, nil
        pcall(function() loading = s.dc.IsLoadingData; lan = s.dc.IsLANLobby end)
        on = loading ~= true
        pcall(function() x0, y0, x1, y1 = globalRect(s.modeOptionsList) end)
        pcall(function() local a, b, c, d = globalRect(s.playButton); bottom = d end)
        x0, y0, x1, y1, bottom = x0 or 0, y0 or 0, x1 or 0, y1 or 0, bottom or 0
        if s ~= OpenLinkUI.lobbySeen then
          OpenLinkUI.lobbySeen = s
          log("lobby screen: IsLoadingData=" .. tostring(loading) .. " IsLANLobby=" .. tostring(lan) ..
              " rows=" .. tostring(x0) .. "," .. tostring(y0) .. "," .. tostring(x1) .. "," .. tostring(y1) .. " play bottom=" .. tostring(bottom))
        end
      end
    end
    OpenLink.SetLobbyVote(on, x0, y0, x1, y1, bottom)
  end
  AddListItemsToCUI = function(items, list, typename, ...)
    if not isLanding(items, typename) then return original(items, list, typename, ...) end
    -- The stock Customs Browser item must always open the official browser.
    if not OpenLinkUI.stockWrapped and type(CustomGamesLandingScreen.OpenCustomGameBrowser) == "function" then
      local stock = CustomGamesLandingScreen.OpenCustomGameBrowser
      CustomGamesLandingScreen.OpenCustomGameBrowser = function(self, ...)
        if type(OpenLink) == "table" and OpenLink.SetBrowserMode then pcall(OpenLink.SetBrowserMode, false) end
        return stock(self, ...)
      end
      OpenLinkUI.stockWrapped = true
    end
    CustomGamesLandingScreen.OpenLinkServers = function(self, sender, args)
      OpenLinkUI.clicks = OpenLinkUI.clicks + 1
      if type(OpenLink) == "table" then pcall(OpenLink.Refresh) end
      -- Preferred: the stock Customs Browser in OpenLink mode (DLL hooks feed our servers).
      if type(OpenLink) == "table" and OpenLink.SetBrowserMode and OpenLink.SetBrowserMode(true) then
        local ok, e = pcall(function() self:LaunchContentScreen(stringid("custom_games_browser")) end)
        if ok then OpenLinkUI.cb = OpenLinkUI.cb + 1; log("customs browser opened in OpenLink mode"); pcall(OpenLinkUI.wrapDispose); return end
        pcall(OpenLink.SetBrowserMode, false); fail("customs browser open", e)
      end
      if OpenLinkUI.launches == 0 then
        -- First try: a second landing page in browse mode (Back returns here).
        OpenLinkUI.browse = true
        OpenLinkUI.launches = 1
        local ok, e = pcall(function() self:LaunchContentScreen(stringid("custom_games_landing")) end)
        if not ok then OpenLinkUI.browse = false; fail("open", e); rebuild(self) else log("browse page requested") end
      else
        -- A browse page that never built must not leak into the next stock visit.
        OpenLinkUI.browse = false
        rebuild(self)
      end
    end
    CustomGamesLandingScreen.OpenLinkRefresh = function(self, sender, args)
      if type(OpenLink) == "table" then pcall(OpenLink.Refresh) end
      rebuild(self)
    end
    if OpenLinkUI.browse then
      OpenLinkUI.browse = false
      local ok, rows = pcall(serverRows)
      if ok then
        local added, result = pcall(original, rows, list, typename, ...)
        if added then OpenLinkUI.built = OpenLinkUI.built + 1; log("browse page built"); return result end
        fail("browse add", result)
      else
        fail("browse rows", rows)
      end
      -- fall through to the stock items
    end
    OpenLinkUI.landing = OpenLinkUI.landing + 1
    local result = original(items, list, typename, ...)
    local item = { Title = "openlink/item/title", Description = "openlink/item/desc", CallbackFunction = "OpenLinkServers", IsEnabled = true }
    local ok, e = pcall(original, { item }, list, typename)
    if not ok then fail("item", e) end
    return result
  end
  log("browser patch installed")
end
