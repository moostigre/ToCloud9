local currentSpec, unlockedSpecs = 1, 1
local dualSpecLevel, tripleSpecLevel, dualSpecPrice = 10, 10, 50
local dualPurchased, tripleEntitled = false, false
local thirdSpecSelected = false
local thirdSpecTab
local unlockButton
local talentUIReady = false

-- Public ownership marker for talent-frame presentation addons.  The launcher
-- may merge all client files into one addon, so IsAddOnLoaded cannot reliably
-- identify this module.
SWPMultispecsEnabled = true
local refreshingThirdSpec = false
local thirdSpecTalents = {}
local thirdSpecFreePoints = 0
local function SendMultispecRequest(command)
    -- The PTR's 3.3.5 client silently drops self-directed addon whispers.
    -- Send a private control whisper instead.  The server hook recognizes and
    -- consumes it before chat delivery, so no command or chat text is shown.
    SendChatMessage("SWPMS " .. string.upper(command), "WHISPER", nil, UnitName("player"))
end
local nativeGetTalentInfo = GetTalentInfo
local nativeGetActiveTalentGroup = GetActiveTalentGroup
local nativeGetUnspentTalentPoints = GetUnspentTalentPoints
local nativeGetTalentTabInfo = GetTalentTabInfo
local nativeGetTalentPrereqs = GetTalentPrereqs
SWPMultispecsVersion = "1.2.6"

StaticPopupDialogs["SWP_MULTISPECS_BUY_DUAL"] = {
    text = "Purchase dual specialization for %d gold?",
    button1 = YES,
    button2 = NO,
    OnAccept = function() SendMultispecRequest("buydual") end,
    timeout = 0,
    whileDead = false,
    hideOnEscape = true,
    preferredIndex = 3,
}

local function GetThirdSpecTalentRank(tabIndex, tier, column)
    return thirdSpecTalents[tabIndex .. ":" .. tier .. ":" .. column] or 0
end

local function GetThirdSpecTreePoints(tabIndex)
    local points = 0
    for key, rank in pairs(thirdSpecTalents) do
        local tree = tonumber(string.match(key, "^(%d+):"))
        if tree == tabIndex then points = points + rank end
    end
    return points
end

local function FindTalentAt(tabIndex, tier, column)
    for talentIndex = 1, (GetNumTalents(tabIndex, false, false) or 0) do
        local _, _, candidateTier, candidateColumn, _, maxRank =
            nativeGetTalentInfo(tabIndex, talentIndex, false, false, 1)
        if candidateTier == tier and candidateColumn == column then
            return talentIndex, maxRank
        end
    end
end

-- The 3.3.5 client only allocates two native talent-group buffers.  Keep the
-- third group virtual for the entire lifetime of the addon: Blizzard queries
-- talents from several callbacks after TalentFrame_Update has returned
-- (prerequisite colouring, tooltips, tabs and click validation).
GetActiveTalentGroup = function(inspect, pet)
    if thirdSpecSelected and not inspect and not pet then return 3 end
    return nativeGetActiveTalentGroup(inspect, pet)
end

GetUnspentTalentPoints = function(inspect, pet, talentGroup)
    if not inspect and not pet and talentGroup == 3 then return thirdSpecFreePoints end
    return nativeGetUnspentTalentPoints(inspect, pet, talentGroup)
end

GetTalentTabInfo = function(tabIndex, inspect, pet, talentGroup)
    if talentGroup ~= 3 then return nativeGetTalentTabInfo(tabIndex, inspect, pet, talentGroup) end
    local info = { nativeGetTalentTabInfo(tabIndex, inspect, pet, 1) }
    info[3] = GetThirdSpecTreePoints(tabIndex)
    info[5] = 0
    return unpack(info)
end

GetTalentPrereqs = function(tabIndex, talentIndex, inspect, pet, talentGroup)
    if talentGroup ~= 3 then
        return nativeGetTalentPrereqs(tabIndex, talentIndex, inspect, pet, talentGroup)
    end

    -- Each prerequisite occupies four return values. The native client can
    -- provide its geometry for group one, but its learnability flags alias
    -- group one too, so replace both flags from the authoritative spec-3 map.
    local prereqs = { nativeGetTalentPrereqs(tabIndex, talentIndex, false, false, 1) }
    for index = 1, #prereqs, 4 do
        local tier, column = prereqs[index], prereqs[index + 1]
        if tier and column then
            local _, maxRank = FindTalentAt(tabIndex, tier, column)
            local learned = GetThirdSpecTalentRank(tabIndex, tier, column) >= (maxRank or 1)
            prereqs[index + 2] = learned
            prereqs[index + 3] = learned
        end
    end
    return unpack(prereqs)
end

GetTalentInfo = function(tabIndex, talentIndex, inspect, pet, talentGroup, ...)
    if talentGroup ~= 3 then
        return nativeGetTalentInfo(tabIndex, talentIndex, inspect, pet, talentGroup, ...)
    end
    local info = { nativeGetTalentInfo(tabIndex, talentIndex, inspect, pet, 1, ...) }
    local tier, column = info[3], info[4]
    info[5] = GetThirdSpecTalentRank(tabIndex, tier, column)
    local treePoints = GetThirdSpecTreePoints(tabIndex)
    local meetsPrereq = treePoints >= ((tier or 1) - 1) * 5
    local prereqs = { GetTalentPrereqs(tabIndex, talentIndex, false, false, 3) }
    for index = 1, #prereqs, 4 do
        if prereqs[index] and not prereqs[index + 2] then meetsPrereq = false end
    end
    info[8] = meetsPrereq
    info[9] = 0
    return unpack(info)
end

local function ThirdSpecTalentClick(self, button)
    local tree = PanelTemplates_GetSelectedTab(PlayerTalentFrame)
    if IsModifiedClick("CHATLINK") then
        local link = GetTalentLink(tree, self:GetID(), false, false, 1, false)
        if link then ChatEdit_InsertLink(link) end
        return
    end
    if button ~= "LeftButton" then return end
    local _, _, _, _, rank, maxRank, _, meetsPrereq =
        GetTalentInfo(tree, self:GetID(), false, false, 3)
    if not meetsPrereq or (rank or 0) >= (maxRank or 0) or thirdSpecFreePoints <= 0 then
        UIErrorsFrame:AddMessage("That talent is not available yet.", 1, 0.1, 0.1)
        return
    end
    -- Use the stock CMSG_LEARN_TALENT path. The core maps the native packet
    -- onto the active extended slot and derives its next rank authoritatively.
    LearnTalent(tree, self:GetID(), false, 1)
end

local function ThirdSpecTalentEnter(self)
    local tree = PanelTemplates_GetSelectedTab(PlayerTalentFrame)
    GameTooltip:SetOwner(self, "ANCHOR_RIGHT")
    -- SetTalent cannot address a third client buffer. Use group one only to
    -- obtain the stock name/description, then replace its aliased rank line.
    GameTooltip:SetTalent(tree, self:GetID(), false, false, 1)
    local _, _, tier, _, rank, maxRank, _, meetsPrereq =
        GetTalentInfo(tree, self:GetID(), false, false, 3)
    if GameTooltipTextLeft2 and maxRank then
        GameTooltipTextLeft2:SetText("Rank " .. (rank or 0) .. "/" .. maxRank)
    end
    -- SetTalent above reads group one's points. Remove that aliased tier warning
    -- and add the requirement calculated from the authoritative third spec.
    for line = 3, GameTooltip:NumLines() do
        local text = _G["GameTooltipTextLeft" .. line]
        local value = text and text:GetText()
        if value and string.find(value, "^Requires %d+ points? in .+ Talents") then
            text:SetText("")
        end
    end
    if not meetsPrereq then
        local required = ((tier or 1) - 1) * 5
        local _, _, spent = GetTalentTabInfo(tree, false, false, 3)
        if (spent or 0) < required then
            local treeName = GetTalentTabInfo(tree, false, false, 3) or "this tree"
            GameTooltip:AddLine("Requires " .. required .. " points in " .. treeName .. " Talents", 1, 0, 0)
        end
    end
    GameTooltip:Show()
end

local function IsThirdSpecCasting()
    local casting = UnitCastingInfo("player")
    local activation = GetSpellInfo(63645)
    return casting and activation and casting == activation
end

local function SkinThirdSpecTab()
    if not thirdSpecTab then return end
    if ElvUI then
        local E = unpack(ElvUI)
        if E and not thirdSpecTab.SWPElvUISkinned then
            -- ElvUI's Talent skin treats specialization icon buttons
            -- differently from ordinary text tabs. Reproduce that exact path;
            -- its stock loop only visits PlayerSpecTab1 through 3.
            local regions = { thirdSpecTab:GetRegions() }
            for _, region in ipairs(regions) do region:Hide() end
            thirdSpecTab:SetTemplate("Default")
            thirdSpecTab:StyleButton(nil, true)
            thirdSpecTab:GetNormalTexture():SetInside()
            thirdSpecTab:GetNormalTexture():SetTexCoord(unpack(E.TexCoords))
            thirdSpecTab:GetNormalTexture():Show()
            thirdSpecTab.SWPElvUISkinned = true
        end
    end
end

local function UpdateSpecTabStates()
    local tabs = { PlayerSpecTab1, PlayerSpecTab2, thirdSpecTab }
    for index, tab in ipairs(tabs) do
        if tab and tab:GetNormalTexture() then
            local active = currentSpec == index
            SetDesaturation(tab:GetNormalTexture(), not active)
            tab:GetNormalTexture():SetAlpha(active and 1 or 0.45)
            tab:GetNormalTexture():SetVertexColor(active and 1 or 0.45,
                active and 1 or 0.45, active and 1 or 0.45)
            tab:SetAlpha(active and 1 or 0.45)
        end
    end
end

local function SetThirdSpecTabShown(shown)
    if not thirdSpecTab then return end
    if shown then
        thirdSpecTab:Show()
    else
        thirdSpecTab:Hide()
    end
end

local function UpdateSpecTabVisibility()
    -- TBC presentation addons can hide the Wrath specialization controls
    -- before this file creates its custom frames.  Reassert the visibility
    -- owned by multispec whenever state or the talent frame is refreshed.
    if PlayerSpecTab1 then
        PlayerSpecTab1:Show()
    end
    if PlayerSpecTab2 then
        if unlockedSpecs >= 2 then
            PlayerSpecTab2:Show()
        else
            PlayerSpecTab2:Hide()
        end
    end
    -- Always expose the third selector. Unlock authorization remains entirely
    -- server-side, so an unentitled client cannot activate it; hiding it made
    -- the control depend on a status message that can arrive before ChatFrame
    -- installs its filter during login.
    SetThirdSpecTabShown(true)
end

local function SyncNativeSpecState()
    -- The server writes the authorized slot count into the native talents
    -- packet. Do not leave the UI at its file-load default while waiting for
    -- an auxiliary status response that may have arrived before ChatFrame was
    -- ready to filter it.
    local nativeCount = GetNumTalentGroups and GetNumTalentGroups(false, false)
    local nativeActive = nativeGetActiveTalentGroup and nativeGetActiveTalentGroup(false)
    if nativeCount and nativeCount > unlockedSpecs then
        unlockedSpecs = nativeCount
    end
    if nativeActive and nativeActive >= 1 then
        currentSpec = nativeActive
    end
end

local function UpdateUnlockButton()
    if not unlockButton then return end

    local level = UnitLevel("player") or 1
    if unlockedSpecs < 2 then
        unlockButton:SetText("Buy Dual Spec (" .. dualSpecPrice .. "g)")
        unlockButton:Show()
        if level >= dualSpecLevel then unlockButton:Enable() else unlockButton:Disable() end
    elseif unlockedSpecs < 3 then
        unlockButton:Show()
        if not tripleEntitled then
            unlockButton:SetText("Triple Spec: Website Shop")
        elseif level < tripleSpecLevel then
            unlockButton:SetText("Triple Spec Requires Level " .. tripleSpecLevel)
        else
            unlockButton:SetText("Refresh Triple Spec")
        end
        unlockButton:Enable()
    else
        unlockButton:Hide()
    end
end

local function UpdateThirdSpecIcon()
    if not thirdSpecTab then return end

    local bestIcon
    local bestPoints = -1
    for tabIndex = 1, MAX_TALENT_TABS do
        local _, icon, pointsSpent, _, previewPointsSpent =
            GetTalentTabInfo(tabIndex, false, false, 3)
        local totalPoints = (pointsSpent or 0) + (previewPointsSpent or 0)
        if icon and totalPoints > bestPoints then
            bestIcon = icon
            bestPoints = totalPoints
        end
    end
    thirdSpecTab:GetNormalTexture():SetTexture(bestIcon or "Interface\\Icons\\Ability_Marksmanship")
end

local function ApplyStableTalentLayout()
    if not PlayerTalentFrame then return end

    PlayerTalentFrame:SetSize(384, 512)
    if PlayerTalentFrameTab4 then PlayerTalentFrameTab4:Hide() end
    if PlayerTalentFramePreviewBar then PlayerTalentFramePreviewBar:Hide() end
    PlayerTalentFramePointsBar:SetPoint("BOTTOM", PlayerTalentFrame, "BOTTOM", 0, 81)

    PlayerTalentFrameActivateButton:ClearAllPoints()
    PlayerTalentFrameActivateButton:SetPoint("TOP", PlayerTalentFrame, "TOP", 0, -42)
    PlayerTalentFrameStatusFrame:ClearAllPoints()
    PlayerTalentFrameStatusFrame:SetPoint("TOP", PlayerTalentFrame, "TOP", 0, -46)

    PlayerTalentFrameSpentPointsText:ClearAllPoints()
    PlayerTalentFrameSpentPointsText:SetPoint("BOTTOMLEFT", PlayerTalentFrame,
        "BOTTOMLEFT", 16, 87)
    PlayerTalentFrameSpentPointsText:SetJustifyH("LEFT")
    PlayerTalentFrameTalentPointsText:ClearAllPoints()
    PlayerTalentFrameTalentPointsText:SetPoint("BOTTOMRIGHT", PlayerTalentFrame,
        "BOTTOMRIGHT", -61, 87)
    PlayerTalentFrameTalentPointsText:SetJustifyH("RIGHT")
end

local function UpdateSpecControls()
    if not talentUIReady or not PlayerTalentFrame:IsShown() then return end
    UpdateSpecTabVisibility()
    local displayedSpec = thirdSpecSelected and 3 or PlayerTalentFrame.talentGroup
    local isActive = displayedSpec == currentSpec
    if isActive then
        PlayerTalentFrameActivateButton:Hide()
        PlayerTalentFrameStatusFrame:Show()
    else
        PlayerTalentFrameStatusFrame:Hide()
        PlayerTalentFrameActivateButton:Show()
        PlayerTalentFrameActivateButton:Enable()
    end

    local preview = GetCVarBool("previewTalents")
    local talentPoints = GetUnspentTalentPoints(false, false, displayedSpec)
    if isActive and talentPoints > 0 and preview then
        PlayerTalentFramePreviewBar:Show()
        if GetGroupPreviewTalentPointsSpent(false, displayedSpec) > 0 then
            PlayerTalentFrameLearnButton:Enable()
            PlayerTalentFrameResetButton:Enable()
        else
            PlayerTalentFrameLearnButton:Disable()
            PlayerTalentFrameResetButton:Disable()
        end
        PlayerTalentFramePointsBar:SetPoint("BOTTOM", PlayerTalentFramePreviewBar, "TOP", 0, -4)
    else
        PlayerTalentFramePreviewBar:Hide()
        -- The scroll frame is anchored to this structural bar. Keep Blizzard's
        -- native offset so talent nodes stop above the independently anchored
        -- spent/unspent labels.
        PlayerTalentFramePointsBar:SetPoint("BOTTOM", PlayerTalentFrame, "BOTTOM", 0, 81)
    end


    if thirdSpecTab then
        PlayerTalentFrameActiveSpecTabHighlight:ClearAllPoints()
        if currentSpec == 3 then
            PlayerTalentFrameActiveSpecTabHighlight:SetParent(thirdSpecTab)
            PlayerTalentFrameActiveSpecTabHighlight:SetAllPoints(thirdSpecTab)
            PlayerTalentFrameActiveSpecTabHighlight:Show()
        else
            PlayerTalentFrameActiveSpecTabHighlight:Hide()
        end
    end
    UpdateSpecTabStates()
    -- Blizzard disables talent buttons when its two-group client state thinks
    -- the displayed group is inactive. Spec 3 uses custom click validation.
    if currentSpec == 3 and thirdSpecSelected then
        local selectedTree = PanelTemplates_GetSelectedTab(PlayerTalentFrame)
        local talentCount = GetNumTalents(selectedTree, false, false) or 0
        for index = 1, talentCount do
            local button = _G["PlayerTalentFrameTalent" .. index]
            if button then
                if not button.SWPNativeOnClick then
                    button.SWPNativeOnClick = button:GetScript("OnClick")
                end
                if not button.SWPNativeOnEnter then
                    button.SWPNativeOnEnter = button:GetScript("OnEnter")
                end
                button:SetScript("OnClick", ThirdSpecTalentClick)
                button:SetScript("OnEnter", ThirdSpecTalentEnter)
                -- Keep every visible talent mouse-enabled so maxed and locked
                -- talents still receive OnEnter and show their tooltips. The
                -- custom click handler above performs the actual validation.
                button:Enable()
            end
        end
    else
        for index = 1, 40 do
            local button = _G["PlayerTalentFrameTalent" .. index]
            if button and button.SWPNativeOnClick then
                button:SetScript("OnClick", button.SWPNativeOnClick)
                button:SetScript("OnEnter", button.SWPNativeOnEnter)
            end
        end
    end
    ApplyStableTalentLayout()
end

local function RefreshThirdSpec()
    if not talentUIReady or not thirdSpecSelected or refreshingThirdSpec then return end
    refreshingThirdSpec = true

    PlayerTalentFrame.pet = false
    PlayerTalentFrame.unit = "player"
    PlayerTalentFrame.talentGroup = 3
    PlayerTalentFrameTitleText:SetText("Third Talent Specialization")
    PlayerSpecTab1:SetChecked(nil)
    PlayerSpecTab2:SetChecked(nil)
    if thirdSpecTab then thirdSpecTab:SetChecked(1) end

    PlayerTalentFrame_UpdateTabs()
    local updateFunction = PlayerTalentFrame.updateFunction
    PlayerTalentFrame.updateFunction = nil
    -- The stock 3.3.5 client only owns two talent-group buffers. Asking its
    -- API for group 3 aliases group 1, so render group 3 with authoritative
    -- ranks sent by the server while retaining Blizzard's native renderer.
    TalentFrame_Update(PlayerTalentFrame)
    PlayerTalentFrame.updateFunction = updateFunction
    if GlyphFrame and GlyphFrame:IsShown() then GlyphFrame_Update() end
    UpdateThirdSpecIcon()
    UpdateSpecControls()

    refreshingThirdSpec = false
end

local function SelectThirdSpec()
    thirdSpecSelected = true
    PlayerSpecTab_OnClick(PlayerSpecTab2)
    thirdSpecSelected = true
    RefreshThirdSpec()
    PlaySound("igCharacterInfoTab")
end

local function CreateThirdSpecTab()
    if talentUIReady or not PlayerTalentFrame or not PlayerSpecTab2 then return end
    talentUIReady = true
    SyncNativeSpecState()

    thirdSpecTab = CreateFrame("CheckButton", "PlayerSpecTab4", PlayerTalentFrame, "PlayerSpecTabTemplate")
    thirdSpecTab:SetID(4)
    thirdSpecTab:SetPoint("TOPLEFT", PlayerSpecTab2, "BOTTOMLEFT", 0, -22)
    thirdSpecTab:SetScript("OnClick", SelectThirdSpec)
    thirdSpecTab:SetScript("OnEnter", function(self)
        GameTooltip:SetOwner(self, "ANCHOR_RIGHT")
        GameTooltip:AddLine("Third Talent Specialization")
        if currentSpec == 3 then
            GameTooltip:AddLine(TALENT_ACTIVE_SPEC_STATUS, GREEN_FONT_COLOR.r,
                GREEN_FONT_COLOR.g, GREEN_FONT_COLOR.b)
        end
        GameTooltip:Show()
    end)
    thirdSpecTab:SetScript("OnLeave", function() GameTooltip:Hide() end)

    unlockButton = CreateFrame("Button", "SWPMultispecsUnlockButton", PlayerTalentFrame,
        "UIPanelButtonTemplate")
    unlockButton:SetWidth(190)
    unlockButton:SetHeight(22)
    unlockButton:SetPoint("BOTTOMRIGHT", PlayerTalentFrame, "BOTTOMRIGHT", -36, 45)
    unlockButton:SetScript("OnClick", function()
        if unlockedSpecs < 2 then
            StaticPopup_Show("SWP_MULTISPECS_BUY_DUAL", dualSpecPrice)
        else
            SendMultispecRequest("buytriple")
        end
    end)
    unlockButton:SetScript("OnEnter", function(self)
        GameTooltip:SetOwner(self, "ANCHOR_RIGHT")
        if unlockedSpecs < 2 then
            GameTooltip:AddLine("Dual Specialization")
            GameTooltip:AddLine("Requires level " .. dualSpecLevel .. " and " ..
                dualSpecPrice .. " gold.", 1, 1, 1, true)
        else
            GameTooltip:AddLine("Triple Specialization")
            GameTooltip:AddLine("Requires level " .. tripleSpecLevel ..
                ", dual specialization, and the character-bound website-shop perk.",
                1, 1, 1, true)
        end
        GameTooltip:Show()
    end)
    unlockButton:SetScript("OnLeave", function() GameTooltip:Hide() end)
    SkinThirdSpecTab()
    UpdateSpecTabStates()

    -- PlayerSpecTab3 is not present in every 3.3.5 TalentUI build.  It is a
    -- secondary Blizzard control (not our third specialization, which is
    -- PlayerSpecTab4), so its absence must not abort multispec initialization.
    if PlayerSpecTab3 then
        PlayerSpecTab3:ClearAllPoints()
        PlayerSpecTab3:SetPoint("TOPLEFT", thirdSpecTab, "BOTTOMLEFT", 0, -39)
    end

    PlayerSpecTab1:HookScript("PreClick", function() thirdSpecSelected = false end)
    PlayerSpecTab2:HookScript("PreClick", function() thirdSpecSelected = false end)
    if PlayerSpecTab3 then
        PlayerSpecTab3:HookScript("PreClick", function() thirdSpecSelected = false end)
    end
    PlayerSpecTab1:HookScript("PostClick", UpdateSpecControls)
    PlayerSpecTab2:HookScript("PostClick", UpdateSpecControls)
    if PlayerSpecTab3 then
        PlayerSpecTab3:HookScript("PostClick", UpdateSpecControls)
    end

    PlayerTalentFrame:HookScript("OnShow", function()
        SyncNativeSpecState()
        if currentSpec == 3 and unlockedSpecs >= 3 then
            thirdSpecSelected = true
            RefreshThirdSpec()
        end
        -- The TBC presentation hook runs first and may have hidden Wrath's
        -- specialization controls before this addon advertised ownership.
        -- Restore the server-authorized tabs and activation state every time
        -- the frame opens, including the primary and secondary specs.
        UpdateSpecTabVisibility()
        UpdateUnlockButton()
        UpdateSpecControls()
    end)

    PlayerTalentFrameActivateButton:SetScript("OnClick", function()
        local displayedSpec = thirdSpecSelected and 3 or
            (PlayerTalentFrame.talentGroup or currentSpec)
        if displayedSpec == currentSpec then
            return
        end
        if thirdSpecSelected then
            SendMultispecRequest("switch 3")
        else
            -- The TBC client data profile cannot reliably start Wrath's native
            -- activation spell. Use the private addon transport for both
            -- native groups and the extended third group; no chat is emitted.
            SendMultispecRequest("switch " .. displayedSpec)
        end
    end)
    PlayerTalentFrameActivateButton:ClearAllPoints()
    PlayerTalentFrameActivateButton:SetPoint("TOP", PlayerTalentFrame,
        "TOP", 0, -42)
    PlayerTalentFrameActivateButton:SetFrameLevel(PlayerTalentFrame:GetFrameLevel() + 10)
    PlayerTalentFrameActivateButton:SetWidth(160)
    PlayerTalentFrameActivateButton:SetText("Activate Specialization")

    -- The active-status message replaces the activation button for the active
    -- specialization. Spent points are positioned separately by the TBC UI.
    PlayerTalentFrameStatusFrame:ClearAllPoints()
    PlayerTalentFrameStatusFrame:SetPoint("TOP", PlayerTalentFrame,
        "TOP", 0, -46)
    ApplyStableTalentLayout()

    if PlayerTalentFrameTab4 then
        PlayerTalentFrameTab4:HookScript("OnShow", function(self)
            self:Hide()
        end)
    end

    local layoutElapsed = 0
    PlayerTalentFrame:HookScript("OnUpdate", function(_, elapsed)
        layoutElapsed = layoutElapsed + elapsed
        if layoutElapsed < 0.05 then return end
        layoutElapsed = 0
        ApplyStableTalentLayout()
    end)

    PlayerTalentFrameActivateButton:HookScript("OnEvent", function(self)
        if thirdSpecSelected then
            if IsThirdSpecCasting() then self:Disable() else self:Enable() end
        end
    end)

    local nativeTalentClick = PlayerTalentFrameTalent_OnClick
    PlayerTalentFrameTalent_OnClick = function(self, button)
        if currentSpec ~= 3 then
            return nativeTalentClick(self, button)
        end
        if IsModifiedClick("CHATLINK") then
            local link = GetTalentLink(PanelTemplates_GetSelectedTab(PlayerTalentFrame), self:GetID(),
                false, false, 3, GetCVarBool("previewTalents"))
            if link then ChatEdit_InsertLink(link) end
        elseif button == "LeftButton" then
            local tree = PanelTemplates_GetSelectedTab(PlayerTalentFrame)
            LearnTalent(tree, self:GetID(), false, 1)
        end
    end

    hooksecurefunc("PlayerTalentFrame_Refresh", RefreshThirdSpec)
    hooksecurefunc("PlayerTalentFrame_Update", function()
        RefreshThirdSpec()
        UpdateSpecControls()
    end)
    SetThirdSpecTabShown(true)
    UpdateSpecTabVisibility()
    UpdateUnlockButton()
    UpdateThirdSpecIcon()
end

ChatFrame_AddMessageEventFilter("CHAT_MSG_SYSTEM", function(_, _, message)
    local snapshotPoints = string.match(message or "", "^MULTISPECS_TALENTS_BEGIN 3 (%d+)$")
    if snapshotPoints then
        thirdSpecTalents = {}
        thirdSpecFreePoints = tonumber(snapshotPoints) or 0
        return true
    end
    local talentData = string.match(message or "", "^MULTISPECS_TALENTS_DATA (.+)$")
    if talentData then
        for tab, tier, column, rank in string.gmatch(talentData, "(%d+),(%d+),(%d+),(%d+);") do
            thirdSpecTalents[tab .. ":" .. tier .. ":" .. column] = tonumber(rank)
        end
        return true
    end
    if message == "MULTISPECS_TALENTS_END 3" then
        if thirdSpecSelected then
            RefreshThirdSpec()
            local focus = GetMouseFocus()
            if focus and focus.SWPNativeOnEnter then ThirdSpecTalentEnter(focus) end
        end
        return true
    end
    local active, count, dualLevel, tripleLevel, price, boughtDual, ownsTriple =
        string.match(message or "", "^MULTISPECS_STATE (%d+) (%d+) (%d+) (%d+) (%d+) (%d+) (%d+)$")
    if not active then return false end

    currentSpec, unlockedSpecs = tonumber(active), tonumber(count)
    dualSpecLevel, tripleSpecLevel, dualSpecPrice = tonumber(dualLevel),
        tonumber(tripleLevel), tonumber(price)
    dualPurchased, tripleEntitled = boughtDual == "1", ownsTriple == "1"
    if thirdSpecTab then
        UpdateSpecTabStates()
        UpdateSpecTabVisibility()
        SetThirdSpecTabShown(true)
        UpdateUnlockButton()
        if unlockedSpecs < 3 and thirdSpecSelected then
            thirdSpecSelected = false
            PlayerSpecTab_OnClick(PlayerSpecTab1)
        elseif currentSpec == 3 and PlayerTalentFrame:IsShown() then
            thirdSpecSelected = true
            RefreshThirdSpec()
        elseif thirdSpecSelected then
            RefreshThirdSpec()
        end
    end
    return true
end)

local events = CreateFrame("Frame")
events:RegisterEvent("ADDON_LOADED")
events:RegisterEvent("PLAYER_LOGIN")
events:RegisterEvent("PLAYER_LEVEL_UP")
events:RegisterEvent("PLAYER_TALENT_UPDATE")
events:RegisterEvent("ACTIVE_TALENT_GROUP_CHANGED")
events:SetScript("OnEvent", function(self, event, addonName)
    if event == "ADDON_LOADED" and addonName == "Blizzard_TalentUI" then
        CreateThirdSpecTab()
    end
    if event == "ADDON_LOADED" and addonName == "ElvUI" then
        SkinThirdSpecTab()
    end
    if talentUIReady and thirdSpecSelected and
        (event == "PLAYER_TALENT_UPDATE" or event == "ACTIVE_TALENT_GROUP_CHANGED") then
        RefreshThirdSpec()
    end
    if event == "PLAYER_TALENT_UPDATE" and currentSpec == 3 then
        -- The event is fired after the client receives the completed reset (or
        -- learn) result. Ask for a new authoritative snapshot here as well as
        -- relying on server hooks, covering trainer resets immediately.
        SendMultispecRequest("status")
    end
    if event == "PLAYER_LOGIN" or event == "PLAYER_LEVEL_UP" or
        event == "ACTIVE_TALENT_GROUP_CHANGED" then
        self.elapsed = 0
        self:SetScript("OnUpdate", function(eventFrame, elapsed)
            eventFrame.elapsed = eventFrame.elapsed + elapsed
            if eventFrame.elapsed >= 1 then
                eventFrame:SetScript("OnUpdate", nil)
                SendMultispecRequest("status")
            end
        end)
    end
end)

if IsAddOnLoaded("Blizzard_TalentUI") then CreateThirdSpecTab() end
