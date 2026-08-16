/*
 * Configurable quality-of-life features for AzerothCore.
 */

#include "Config.h"
#include "Item.h"
#include "ItemTemplate.h"
#include "Log.h"
#include "Map.h"
#include "ObjectMgr.h"
#include "Player.h"
#include "ScriptMgr.h"
#include "SpellAuraEffects.h"
#include "SpellAuras.h"
#include "SpellMgr.h"
#include "SpellScript.h"
#include "WorldSession.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <unordered_map>
#include <unordered_set>

namespace
{
struct QolConfig
{
    bool enabled = true;
    uint32 hearthstoneCooldownMinutes = 15;
    float outOfCombatRunSpeed = 1.20f;
    uint32 outOfCombatRunIndicatorSpell = 80861;
    uint32 outOfCombatRunPvPLockoutSeconds = 30;
    uint32 stackSizeMultiplier = 5;
    uint32 maximumStackSize = 1000;
    float bagSlotIncreasePercent = 25.0f;
    float mountItemPriceMultiplier = 0.25f;
    uint32 foodBuffDurationMinutes = 60;
    uint32 scrollBuffDurationMinutes = 60;
    uint32 initiateRidingSpell = 80860;
    uint16 initiateRidingSkillValue = 50;

    void Load()
    {
        enabled = sConfigMgr->GetOption<bool>("Qol.Enable", true);
        hearthstoneCooldownMinutes = sConfigMgr->GetOption<uint32>("Qol.HearthstoneCooldownMinutes", 15);
        outOfCombatRunSpeed = std::clamp(sConfigMgr->GetOption<float>("Qol.OutOfCombatRunSpeed", 1.20f), 1.0f, 10.0f);
        outOfCombatRunIndicatorSpell = sConfigMgr->GetOption<uint32>("Qol.OutOfCombatRunIndicatorSpell", 80861);
        outOfCombatRunPvPLockoutSeconds =
            sConfigMgr->GetOption<uint32>("Qol.OutOfCombatRunPvPLockoutSeconds", 30);
        stackSizeMultiplier = std::max<uint32>(1, sConfigMgr->GetOption<uint32>("Qol.StackSizeMultiplier", 5));
        maximumStackSize = std::max<uint32>(1, sConfigMgr->GetOption<uint32>("Qol.MaximumStackSize", 1000));
        bagSlotIncreasePercent = std::clamp(
            sConfigMgr->GetOption<float>("Qol.BagSlotIncreasePercent", 25.0f), 0.0f, 1000.0f);
        mountItemPriceMultiplier = std::clamp(sConfigMgr->GetOption<float>("Qol.MountItemPriceMultiplier", 0.25f), 0.0f, 1.0f);
        foodBuffDurationMinutes = sConfigMgr->GetOption<uint32>("Qol.FoodBuffDurationMinutes", 60);
        scrollBuffDurationMinutes = sConfigMgr->GetOption<uint32>("Qol.ScrollBuffDurationMinutes", 60);
        initiateRidingSpell = sConfigMgr->GetOption<uint32>("Qol.InitiateRidingSpell", 80860);
        initiateRidingSkillValue = std::clamp<uint16>(
            sConfigMgr->GetOption<uint16>("Qol.InitiateRidingSkillValue", 50), 1, 74);
    }
};

QolConfig config;
std::unordered_set<uint32> foodBuffSpellIds;
std::unordered_set<uint32> scrollBuffSpellIds;
std::unordered_map<uint32, uint32> pvpRunLockoutTimers;
std::unordered_set<uint32> playersInCombat;

void ApplyInitiateRiding(Player* player)
{
    if (!config.enabled || !player || player->GetLevel() < 10 || !config.initiateRidingSpell)
        return;

    constexpr uint32 apprenticeRidingSpell = 33388;
    if (!player->HasSpell(config.initiateRidingSpell))
        player->learnSpell(config.initiateRidingSpell, false);

    constexpr uint16 ridingSkill = SKILL_RIDING;
    // Spell 80860 is a client-visible trainer marker without a SKILL_STEP
    // effect. The server owns the 50-point cap so Apprentice Riding's
    // achievement criteria never see a transient 75-point skill.
    if (!player->HasSpell(apprenticeRidingSpell) &&
        player->GetPureMaxSkillValue(ridingSkill) <= 75)
        player->SetSkill(ridingSkill, 1, config.initiateRidingSkillValue, config.initiateRidingSkillValue);
}

void AddSpellAndTriggeredSpells(uint32 spellId, std::unordered_set<uint32>& spellIds)
{
    if (!spellId || !spellIds.insert(spellId).second)
        return;

    SpellInfo const* spellInfo = sSpellMgr->GetSpellInfo(spellId);
    if (!spellInfo)
        return;

    for (SpellEffectInfo const& effect : spellInfo->Effects)
        AddSpellAndTriggeredSpells(effect.TriggerSpell, spellIds);
}

bool CanModifyRunSpeedAura(Player* player)
{
    return player &&
        player->IsInWorld() &&
        !player->IsBeingTeleported() &&
        player->GetMap() &&
        player->GetSession() &&
        !player->GetSession()->PlayerLogout();
}

void RemoveRunSpeedAura(Player* player)
{
    if (!config.outOfCombatRunIndicatorSpell || !CanModifyRunSpeedAura(player))
        return;

    if (Aura* aura = player->GetAura(config.outOfCombatRunIndicatorSpell, player->GetGUID()))
        if (aura->GetMaxDuration() < 0)
            player->RemoveAura(aura);
}

void StartPvPRunLockout(Player* player)
{
    if (!player)
        return;

    pvpRunLockoutTimers[player->GetGUID().GetCounter()] =
        config.outOfCombatRunPvPLockoutSeconds * IN_MILLISECONDS;
    RemoveRunSpeedAura(player);
}

bool IsMountSpell(SpellInfo const* spellInfo, uint8 depth = 0)
{
    if (!spellInfo || depth > 4)
        return false;

    if (spellInfo->HasAura(SPELL_AURA_MOUNTED))
        return true;

    for (SpellEffectInfo const& effect : spellInfo->Effects)
        if (effect.TriggerSpell && IsMountSpell(sSpellMgr->GetSpellInfo(effect.TriggerSpell), depth + 1))
            return true;

    return false;
}

bool IsMountItem(ItemTemplate const& item)
{
    for (auto const& itemSpell : item.Spells)
    {
        if (itemSpell.SpellId <= 0)
            continue;

        if (IsMountSpell(sSpellMgr->GetSpellInfo(static_cast<uint32>(itemSpell.SpellId))))
            return true;
    }

    return false;
}

class QolWorldScript final : public WorldScript
{
public:
    QolWorldScript() : WorldScript("QolWorldScript",
    {
        WORLDHOOK_ON_BEFORE_CONFIG_LOAD,
        WORLDHOOK_ON_BEFORE_WORLD_INITIALIZED
    }) { }

    void OnBeforeConfigLoad(bool /*reload*/) override
    {
        config.Load();
        LOG_INFO("module", "mod-qol: enabled={}, taxi speed={}x, out-of-combat run speed={}x",
            config.enabled,
            sConfigMgr->GetOption<float>("Qol.TaxiSpeedMultiplier", 1.0f),
            config.outOfCombatRunSpeed);
    }

    void OnBeforeWorldInitialized() override
    {
        if (!config.enabled)
            return;

        uint32 stackItems = 0;
        uint32 bags = 0;
        uint32 mountItems = 0;
        foodBuffSpellIds.clear();
        scrollBuffSpellIds.clear();

        for (ItemTemplate* item : *sObjectMgr->GetItemTemplateStoreFast())
        {
            if (!item)
                continue;

            std::unordered_set<uint32>* consumableSpellIds = nullptr;
            if (item->Class == ITEM_CLASS_CONSUMABLE)
            {
                if (item->SubClass == ITEM_SUBCLASS_FOOD)
                    consumableSpellIds = &foodBuffSpellIds;
                else if (item->SubClass == ITEM_SUBCLASS_SCROLL)
                    consumableSpellIds = &scrollBuffSpellIds;
            }

            if (consumableSpellIds)
            {
                for (auto const& itemSpell : item->Spells)
                    if (itemSpell.SpellId > 0)
                        AddSpellAndTriggeredSpells(static_cast<uint32>(itemSpell.SpellId), *consumableSpellIds);
            }

            if (item->Stackable > 1 && config.stackSizeMultiplier > 1)
            {
                uint64 enlarged = uint64(item->Stackable) * config.stackSizeMultiplier;
                item->Stackable = static_cast<int32>(std::min<uint64>(enlarged, config.maximumStackSize));
                ++stackItems;
            }

            if (item->ContainerSlots && config.bagSlotIncreasePercent > 0.0f)
            {
                float multiplier = 1.0f + config.bagSlotIncreasePercent / 100.0f;
                uint32 enlargedSlots = static_cast<uint32>(std::ceil(item->ContainerSlots * multiplier));
                uint32 wantedSlots = std::min<uint32>(enlargedSlots, MAX_BAG_SIZE);

                if (wantedSlots > item->ContainerSlots)
                {
                    item->ContainerSlots = wantedSlots;
                    ++bags;
                }
            }

            if (item->BuyPrice && config.mountItemPriceMultiplier < 1.0f && IsMountItem(*item))
            {
                item->BuyPrice = std::max<uint32>(1, static_cast<uint32>(item->BuyPrice * config.mountItemPriceMultiplier));
                ++mountItems;
            }
        }

        LOG_INFO("module", "mod-qol: enlarged {} stackable items and {} bags; discounted {} mount items",
            stackItems, bags, mountItems);
        LOG_INFO("module", "mod-qol: indexed {} food and {} scroll spells for configurable buff durations",
            foodBuffSpellIds.size(), scrollBuffSpellIds.size());
    }
};

class QolPlayerScript final : public PlayerScript
{
public:
    QolPlayerScript() : PlayerScript("QolPlayerScript",
    {
        PLAYERHOOK_ON_LOGIN,
        PLAYERHOOK_ON_LOGOUT,
        PLAYERHOOK_ON_UPDATE,
        PLAYERHOOK_ON_LEVEL_CHANGED,
        PLAYERHOOK_ON_LEARN_SPELL,
        PLAYERHOOK_ON_DUEL_START,
        PLAYERHOOK_ON_PLAYER_ENTER_COMBAT,
        PLAYERHOOK_ON_PLAYER_LEAVE_COMBAT
    }) { }

    void OnPlayerLogin(Player* player) override
    {
        ApplyInitiateRiding(player);
        uint32 guid = player->GetGUID().GetCounter();
        updateTimers[guid] = 0;
        pvpRunLockoutTimers[guid] = 0;
        if (player->IsInCombat())
            playersInCombat.insert(guid);
        else
            playersInCombat.erase(guid);
        UpdateRunSpeedAura(player);
        LOG_DEBUG("module", "mod-qol: applied run-speed aura check to {} (combat={}, mounted={}, taxi={})",
            player->GetName(), player->IsInCombat(), player->IsMounted(), player->IsInFlight());
    }

    void OnPlayerLevelChanged(Player* player, uint8 oldLevel) override
    {
        if (oldLevel < 10 && player->GetLevel() >= 10)
            ApplyInitiateRiding(player);
    }

    void OnPlayerLearnSpell(Player* player, uint32 spellId) override
    {
        if (spellId == config.initiateRidingSpell)
            ApplyInitiateRiding(player);
    }

    void OnPlayerUpdate(Player* player, uint32 diff) override
    {
        uint32 guid = player->GetGUID().GetCounter();
        uint32& pvpLockout = pvpRunLockoutTimers[guid];
        pvpLockout = pvpLockout > diff ? pvpLockout - diff : 0;

        uint32& timer = updateTimers[guid];
        if (timer > diff)
        {
            timer -= diff;
            return;
        }

        timer = 500;
        UpdateRunSpeedAura(player);
    }

    void OnPlayerLogout(Player* player) override
    {
        updateTimers.erase(player->GetGUID().GetCounter());
        pvpRunLockoutTimers.erase(player->GetGUID().GetCounter());
        playersInCombat.erase(player->GetGUID().GetCounter());
    }

    void OnPlayerDuelStart(Player* player1, Player* player2) override
    {
        RemoveRunSpeedAura(player1);
        RemoveRunSpeedAura(player2);
    }

    void OnPlayerEnterCombat(Player* player, Unit* /*enemy*/) override
    {
        playersInCombat.insert(player->GetGUID().GetCounter());
        RemoveRunSpeedAura(player);
    }

    void OnPlayerLeaveCombat(Player* player) override
    {
        playersInCombat.erase(player->GetGUID().GetCounter());
        UpdateRunSpeedAura(player);
    }

private:
    static void UpdateRunSpeedAura(Player* player)
    {
        // Player update hooks may run while a teleport or clustered map
        // transfer is tearing down the old player object. Adding or removing
        // an aura during that window trips Unit::_AddAura's cleanup assertion.
        if (!CanModifyRunSpeedAura(player))
            return;

        uint32 spellId = config.outOfCombatRunIndicatorSpell;
        if (!config.enabled || !spellId || config.outOfCombatRunSpeed <= 1.0f)
        {
            RemoveRunSpeedAura(player);
            return;
        }

        bool pvpLocked = pvpRunLockoutTimers[player->GetGUID().GetCounter()] > 0;
        bool combatTracked = playersInCombat.contains(player->GetGUID().GetCounter());
        bool bonusActive = player->IsAlive() &&
            !player->IsInCombat() &&
            !combatTracked &&
            !player->IsMounted() &&
            !player->IsInFlight() &&
            !player->HasAuraType(SPELL_AURA_MOD_STEALTH) &&
            !player->GetMap()->IsDungeon() &&
            !player->GetMap()->IsRaid() &&
            !player->InBattleground() &&
            !player->InArena() &&
            !player->duel &&
            !pvpLocked;
        Aura* indicator = player->GetAura(spellId, player->GetGUID());

        if (bonusActive)
        {
            if (!indicator)
            {
                player->CastSpell(player, spellId, true);
                indicator = player->GetAura(spellId, player->GetGUID());
                if (indicator)
                {
                    int32 speedPercent = static_cast<int32>(
                        std::lround((config.outOfCombatRunSpeed - 1.0f) * 100.0f));
                    if (AuraEffect* speedEffect = indicator->GetEffect(EFFECT_0))
                        speedEffect->ChangeAmount(speedPercent);

                    // A negative duration makes the client display a permanent buff.
                    indicator->SetMaxDuration(-1);
                    indicator->SetDuration(-1);
                }
            }

            if (!indicator)
                LOG_ERROR("module", "mod-qol: could not apply run-speed indicator spell {}", spellId);
        }
        else
            RemoveRunSpeedAura(player);
    }

    std::unordered_map<uint32, uint32> updateTimers;
};

class QolUnitScript final : public UnitScript
{
public:
    QolUnitScript() : UnitScript("QolUnitScript", true,
        { UNITHOOK_ON_DAMAGE, UNITHOOK_ON_AURA_APPLY }) { }

    void OnDamage(Unit* attacker, Unit* victim, uint32& damage) override
    {
        if (!config.enabled || !damage || !attacker || !victim)
            return;

        Player* attackerPlayer = attacker->GetCharmerOrOwnerPlayerOrPlayerItself();
        Player* victimPlayer = victim->GetCharmerOrOwnerPlayerOrPlayerItself();
        if (!attackerPlayer || !victimPlayer || attackerPlayer == victimPlayer)
            return;

        StartPvPRunLockout(attackerPlayer);
        StartPvPRunLockout(victimPlayer);
    }

    void OnAuraApply(Unit* unit, Aura* aura) override
    {
        if (!config.enabled || !unit || !unit->IsPlayer() || !aura || aura->GetMaxDuration() <= 0)
            return;

        uint32 durationMinutes = 0;
        uint32 spellId = aura->GetId();
        SpellSpecificType spellSpecific = aura->GetSpellInfo()->GetSpellSpecific();

        if (foodBuffSpellIds.contains(spellId) ||
            spellSpecific == SPELL_SPECIFIC_FOOD ||
            spellSpecific == SPELL_SPECIFIC_FOOD_AND_DRINK)
        {
            durationMinutes = config.foodBuffDurationMinutes;
        }
        else if (scrollBuffSpellIds.contains(spellId) || spellSpecific == SPELL_SPECIFIC_SCROLL)
            durationMinutes = config.scrollBuffDurationMinutes;

        // Zero keeps AzerothCore's original duration for this category.
        if (!durationMinutes)
            return;

        uint64 duration = uint64(durationMinutes) * MINUTE * IN_MILLISECONDS;
        int32 durationMilliseconds = static_cast<int32>(
            std::min<uint64>(duration, static_cast<uint64>(std::numeric_limits<int32>::max())));
        aura->SetMaxDuration(durationMilliseconds);
        aura->SetDuration(durationMilliseconds);
    }
};

class QolHearthstoneSpellScript final : public SpellScript
{
    PrepareSpellScript(QolHearthstoneSpellScript);

    void HandleAfterCast()
    {
        if (!config.enabled)
            return;

        if (Player* player = GetCaster()->ToPlayer())
        {
            constexpr uint32 defaultCooldown = 30 * MINUTE * IN_MILLISECONDS;
            uint32 wantedCooldown = config.hearthstoneCooldownMinutes * MINUTE * IN_MILLISECONDS;
            player->ModifySpellCooldown(GetSpellInfo()->Id, static_cast<int32>(wantedCooldown) - static_cast<int32>(defaultCooldown));
        }
    }

    void Register() override
    {
        AfterCast += SpellCastFn(QolHearthstoneSpellScript::HandleAfterCast);
    }
};
}

void Addmod_qolScripts()
{
    new QolWorldScript();
    new QolPlayerScript();
    new QolUnitScript();
    RegisterSpellScript(QolHearthstoneSpellScript);
}
