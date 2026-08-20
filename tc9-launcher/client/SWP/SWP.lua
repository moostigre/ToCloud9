local INITIATE_RIDING_SPELL = 80860
local SWP_DUNGEON_HARDCORE = "DUNGEON_DIFFICULTY3"
local SWP_RAID_HARDCORE_10 = "SWP_RAID_HARDCORE_10"
local SWP_RAID_HARDCORE_25 = "SWP_RAID_HARDCORE_25"

SWPInstanceDifficultyDB = SWPInstanceDifficultyDB or { mode = "normal" }

local function SendDifficultyPacket(payload)
    SendAddonMessage("SWPIDIFF", payload, "WHISPER", UnitName("player"))
end

local function RequestDifficultyState()
    SendDifficultyPacket("STATE dungeon")
    SendDifficultyPacket("STATE raid")
end

local function CanSelectDifficulty(message)
    local raidMembers = GetNumRaidMembers()
    local partyMembers = GetNumPartyMembers()
    if raidMembers > 0 and not IsRaidLeader() then
        UIErrorsFrame:AddMessage(message, 1, 0.1, 0.1)
        return false
    elseif raidMembers == 0 and partyMembers > 0 and not IsPartyLeader() then
        UIErrorsFrame:AddMessage(message, 1, 0.1, 0.1)
        return false
    end
    return true
end

local function SynchronizeDungeonProfile(profile)
    if not CanSelectDifficulty("Only the group leader can change instance difficulty.") then return end

    SWPInstanceDifficultyDB.mode = profile
    SendDifficultyPacket("SET dungeon " .. profile)
end

local function SelectDungeonHardcore()
    if not CanSelectDifficulty("Only the group leader can change instance difficulty.") then return end
    SWPInstanceDifficultyDB.mode = "hardcore"
    SendDifficultyPacket("SET dungeon hardcore")
end

local function SelectRaidHardcore(size)
    if not CanSelectDifficulty("Only the group leader can change raid difficulty.") then return end
    SWPInstanceDifficultyDB.raidMode = "hardcore"
    SWPInstanceDifficultyDB.raidSize = size
    SetRaidDifficulty(size == 10 and 3 or 4)
    SendDifficultyPacket("SET raid hardcore")
end

local function SynchronizeRaidProfile(profile, size)
    if not CanSelectDifficulty("Only the group leader can change raid difficulty.") then return end
    SWPInstanceDifficultyDB.raidMode = profile
    SWPInstanceDifficultyDB.raidSize = size
    SendDifficultyPacket("SET raid " .. profile)
end

local function AddDifficultyButton(menuName, value)
    local menu = UnitPopupMenus[menuName]
    if not menu then return end
    for _, existing in ipairs(menu) do
        if existing == value then return end
    end
    table.insert(menu, value)
end

UnitPopupButtons[SWP_DUNGEON_HARDCORE] = {
    text = "5 Player (Hardcore)",
    dist = 0,
}
UnitPopupButtons[SWP_RAID_HARDCORE_10] = { text = "10 Player (Hardcore)", dist = 0 }
UnitPopupButtons[SWP_RAID_HARDCORE_25] = { text = "25 Player (Hardcore)", dist = 0 }

AddDifficultyButton("DUNGEON_DIFFICULTY", SWP_DUNGEON_HARDCORE)
AddDifficultyButton("RAID_DIFFICULTY", SWP_RAID_HARDCORE_10)
AddDifficultyButton("RAID_DIFFICULTY", SWP_RAID_HARDCORE_25)

-- Restore Blizzard's dungeon-difficulty submenu below level 65.
local function ShowLowLevelDungeonDifficultyMenu()
    local dropdown = UIDROPDOWNMENU_INIT_MENU
    local level = UIDROPDOWNMENU_MENU_LEVEL
    if not dropdown or not dropdown.which or not level then
        return
    end

    local menu = UnitPopupMenus[dropdown.which]
    local shown = UnitPopupShown[level]
    if not menu or not shown then
        return
    end

    for index, value in ipairs(menu) do
        if value == "DUNGEON_DIFFICULTY" or value == "RAID_DIFFICULTY" then
            shown[index] = 1
        end
    end
end

hooksecurefunc("UnitPopup_HideButtons", ShowLowLevelDungeonDifficultyMenu)

-- FrameXML only knows how to check its two stock dungeon entries. Use the
-- selected virtual profile as the authority for all three menu entries.
local function RefreshDungeonDifficultyChecks()
    local level = UIDROPDOWNMENU_MENU_LEVEL or 1
    local list = _G["DropDownList" .. level]
    if not list or not list:IsShown() then return end

    for index = 1, UIDROPDOWNMENU_MAXBUTTONS do
        local button = _G["DropDownList" .. level .. "Button" .. index]
        if button and button:IsShown() then
            local selected =
                (button.value == "DUNGEON_DIFFICULTY1" and SWPInstanceDifficultyDB.mode == "normal") or
                (button.value == "DUNGEON_DIFFICULTY2" and SWPInstanceDifficultyDB.mode == "heroic") or
                (button.value == SWP_DUNGEON_HARDCORE and SWPInstanceDifficultyDB.mode == "hardcore")
            if button.value == "DUNGEON_DIFFICULTY1" or
               button.value == "DUNGEON_DIFFICULTY2" or
               button.value == SWP_DUNGEON_HARDCORE then
                local check = _G[button:GetName() .. "Check"]
                local uncheck = _G[button:GetName() .. "UnCheck"]
                if check then if selected then check:Show() else check:Hide() end end
                if uncheck then uncheck:Hide() end
            end
        end
    end
end

local function HandleDifficultyButton(button)
    if button.value == SWP_DUNGEON_HARDCORE then
        SelectDungeonHardcore()
    elseif button.value == SWP_RAID_HARDCORE_10 then
        SelectRaidHardcore(10)
    elseif button.value == SWP_RAID_HARDCORE_25 then
        SelectRaidHardcore(25)
    elseif button.value == "DUNGEON_DIFFICULTY1" then
        SynchronizeDungeonProfile("normal")
    elseif button.value == "DUNGEON_DIFFICULTY2" then
        SynchronizeDungeonProfile("heroic")
    elseif button.value == "RAID_DIFFICULTY1" then
        SynchronizeRaidProfile("normal", 10)
    elseif button.value == "RAID_DIFFICULTY2" then
        SynchronizeRaidProfile("normal", 25)
    elseif button.value == "RAID_DIFFICULTY3" then
        SynchronizeRaidProfile("heroic", 10)
    elseif button.value == "RAID_DIFFICULTY4" then
        SynchronizeRaidProfile("heroic", 25)
    end
end

local BlizzardUnitPopupOnClick = UnitPopup_OnClick
function UnitPopup_OnClick(button)
    if button and button.value == SWP_DUNGEON_HARDCORE then
        HandleDifficultyButton(button)
        CloseDropDownMenus()
        return
    end

    BlizzardUnitPopupOnClick(button)
    if button then HandleDifficultyButton(button) end
end

local BlizzardUnitPopupShowMenu = UnitPopup_ShowMenu
function UnitPopup_ShowMenu(...)
    local NativeGetDungeonDifficulty = GetDungeonDifficulty
    GetDungeonDifficulty = function()
        if SWPInstanceDifficultyDB.mode == "hardcore" then return 3 end
        if SWPInstanceDifficultyDB.mode == "heroic" then return 2 end
        return 1
    end
    BlizzardUnitPopupShowMenu(...)
    GetDungeonDifficulty = NativeGetDungeonDifficulty
end

local difficultyState = CreateFrame("Frame")
local nativeWelcomePrefix = string.match(RAID_INSTANCE_WELCOME or "", "^(.-)%%") or ""
local suppressNativeWelcomeUntil = 0
local managedInstanceNames = {
    ["Ragefire Chasm"] = true,
    ["Wailing Caverns"] = true,
    ["Shadowfang Keep"] = true,
}

local function IsManagedInstanceWelcome(message)
    local instanceName = GetInstanceInfo()
    local text = tostring(message or "")
    return managedInstanceNames[instanceName] and
        string.find(text, "Instance locks are scheduled to expire", 1, true)
end

local function FormatVirtualLockTime(totalSeconds)
    local remaining = math.max(0, tonumber(totalSeconds) or 0)
    local units = {
        { 86400, "day" },
        { 3600, "hour" },
        { 60, "minute" },
    }
    local parts = {}
    for _, unit in ipairs(units) do
        local count = math.floor(remaining / unit[1])
        if count > 0 then
            table.insert(parts, count .. " " .. unit[2] .. (count == 1 and "" or "s"))
            remaining = remaining - count * unit[1]
            if #parts == 2 then break end
        end
    end
    return #parts > 0 and table.concat(parts, " ") or "less than a minute"
end

ChatFrame_AddMessageEventFilter("CHAT_MSG_SYSTEM", function(_, _, message)
    if IsManagedInstanceWelcome(message) or
        (GetTime() <= suppressNativeWelcomeUntil and nativeWelcomePrefix ~= "" and
         string.sub(message or "", 1, string.len(nativeWelcomePrefix)) == nativeWelcomePrefix) then
        return true
    end
    return false
end)

local BlizzardDefaultChatAddMessage = DEFAULT_CHAT_FRAME.AddMessage
DEFAULT_CHAT_FRAME.AddMessage = function(frame, message, ...)
    local text = tostring(message or "")
    if IsManagedInstanceWelcome(text) or (GetTime() <= suppressNativeWelcomeUntil and
        (string.find(text, "Instance locks are scheduled to expire", 1, true) or
         (nativeWelcomePrefix ~= "" and string.sub(text, 1, string.len(nativeWelcomePrefix)) == nativeWelcomePrefix))) then
        return
    end
    return BlizzardDefaultChatAddMessage(frame, message, ...)
end

difficultyState:RegisterEvent("CHAT_MSG_ADDON")
difficultyState:RegisterEvent("PLAYER_LOGIN")
difficultyState:SetScript("OnEvent", function(self, event, prefix, message)
    if event == "PLAYER_LOGIN" then
        RegisterAddonMessagePrefix("SWPIDIFF")
        self.elapsed = 0
        self:SetScript("OnUpdate", function(frame, elapsed)
            frame.elapsed = frame.elapsed + elapsed
            if frame.elapsed >= 1 then
                frame:SetScript("OnUpdate", nil)
                RequestDifficultyState()
            end
        end)
        return
    end
    if prefix ~= "SWPIDIFF" or not message then return end
    local profile, seconds = string.match(message, "^WELCOME\t([^\t]+)\t(%d+)$")
    if profile and seconds then
        local instanceName = GetInstanceInfo()
        local remaining = FormatVirtualLockTime(seconds)
        suppressNativeWelcomeUntil = GetTime() + 10
        DEFAULT_CHAT_FRAME:AddMessage("|cffffd100Welcome to " .. (instanceName or "this instance") ..
            " (" .. profile .. "). Virtual lock expires in " .. remaining .. ".|r")
        return
    end
    local mode = string.match(message, "^STATE dungeon (%w+)$")
    if mode == "normal" or mode == "heroic" or mode == "hardcore" then
        SWPInstanceDifficultyDB.mode = mode
        return
    end
    mode = string.match(message, "^STATE raid (%w+)$")
    if mode == "normal" or mode == "heroic" or mode == "hardcore" then
        SWPInstanceDifficultyDB.raidMode = mode
    end
end)

local alert = CreateFrame("Frame", "SWPInitiateRidingAlert", UIParent)
alert:SetSize(460, 164)
alert:SetPoint("CENTER", UIParent, "CENTER", 0, 150)
alert:SetFrameStrata("DIALOG")
alert:SetClampedToScreen(true)
alert:SetBackdrop({
    bgFile = "Interface\\Tooltips\\UI-Tooltip-Background",
    edgeFile = "Interface\\Tooltips\\UI-Tooltip-Border",
    tile = true,
    tileSize = 16,
    edgeSize = 18,
    insets = { left = 5, right = 5, top = 5, bottom = 5 },
})
alert:SetBackdropColor(0.055, 0.025, 0.012, 0.98)
alert:SetBackdropBorderColor(0.72, 0.53, 0.22, 1)
alert:Hide()

alert.accent = alert:CreateTexture(nil, "ARTWORK")
alert.accent:SetTexture("Interface\\ChatFrame\\ChatFrameBackground")
alert.accent:SetVertexColor(0.85, 0.58, 0.15, 0.9)
alert.accent:SetPoint("TOPLEFT", 18, -18)
alert.accent:SetPoint("TOPRIGHT", -18, -18)
alert.accent:SetHeight(1)

alert.iconBorder = CreateFrame("Frame", nil, alert)
alert.iconBorder:SetSize(74, 74)
alert.iconBorder:SetPoint("TOPLEFT", 24, -30)
alert.iconBorder:SetBackdrop({
    bgFile = "Interface\\Tooltips\\UI-Tooltip-Background",
    edgeFile = "Interface\\Tooltips\\UI-Tooltip-Border",
    tile = true,
    tileSize = 8,
    edgeSize = 14,
    insets = { left = 3, right = 3, top = 3, bottom = 3 },
})
alert.iconBorder:SetBackdropColor(0.02, 0.01, 0.005, 1)
alert.iconBorder:SetBackdropBorderColor(0.82, 0.62, 0.25, 1)

alert.icon = alert.iconBorder:CreateTexture(nil, "ARTWORK")
alert.icon:SetPoint("TOPLEFT", 7, -7)
alert.icon:SetPoint("BOTTOMRIGHT", -7, 7)
alert.icon:SetTexture(GetSpellTexture(INITIATE_RIDING_SPELL) or "Interface\\Icons\\Ability_Mount_RidingHorse")

alert.title = alert:CreateFontString(nil, "OVERLAY", "GameFontNormalLarge")
alert.title:SetPoint("TOPLEFT", 112, -27)
alert.title:SetPoint("RIGHT", alert, "RIGHT", -28, 0)
alert.title:SetJustifyH("LEFT")
alert.title:SetText("Initiate Riding learned!")
alert.title:SetTextColor(1, 0.82, 0)

alert.message = alert:CreateFontString(nil, "OVERLAY", "GameFontHighlight")
alert.message:SetPoint("TOPLEFT", alert.title, "BOTTOMLEFT", 0, -8)
alert.message:SetPoint("RIGHT", alert, "RIGHT", -28, 0)
alert.message:SetHeight(42)
alert.message:SetJustifyH("LEFT")
alert.message:SetJustifyV("TOP")
alert.message:SetText("You can now ride tiny mounts. Visit your race's mount vendor and choose your first companion!")

alert.close = CreateFrame("Button", nil, alert, "UIPanelButtonTemplate")
alert.close:SetSize(120, 26)
alert.close:SetPoint("BOTTOM", alert, "BOTTOM", 0, 20)
alert.close:SetText("Ok, super!")
alert.close:SetScript("OnClick", function()
    SWPInitiateRidingSeen = true
    alert:Hide()
end)

local function ShowRidingAlert()
    if SWPInitiateRidingSeen or UnitLevel("player") < 10 then
        return
    end
    alert.icon:SetTexture(GetSpellTexture(INITIATE_RIDING_SPELL) or "Interface\\Icons\\Ability_Mount_RidingHorse")
    PlaySound("igQuestListComplete")
    alert:Show()
end

local function ScheduleRidingAlert(frame, delay)
    frame.ridingAlertDelay = delay
    frame:SetScript("OnUpdate", function(self, elapsed)
        self.ridingAlertDelay = self.ridingAlertDelay - elapsed
        if self.ridingAlertDelay <= 0 then
            self:SetScript("OnUpdate", nil)
            ShowRidingAlert()
        end
    end)
end

local events = CreateFrame("Frame")
events:RegisterEvent("PLAYER_LOGIN")
events:RegisterEvent("PLAYER_LEVEL_UP")
events:SetScript("OnEvent", function(self, event, level)
    if event == "PLAYER_LEVEL_UP" and tonumber(level) == 10 then
        -- PLAYER_LEVEL_UP can arrive before UnitLevel reflects the new level.
        ScheduleRidingAlert(self, 0.75)
    elseif event == "PLAYER_LOGIN" and UnitLevel("player") >= 10 and not SWPInitiateRidingSeen then
        ScheduleRidingAlert(self, 2)
    end
end)
