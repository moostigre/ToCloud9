local INITIATE_RIDING_SPELL = 80860

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
        ScheduleRidingAlert(self, 0.75)
    elseif event == "PLAYER_LOGIN" and UnitLevel("player") >= 10 and not SWPInitiateRidingSeen then
        ScheduleRidingAlert(self, 2)
    end
end)
