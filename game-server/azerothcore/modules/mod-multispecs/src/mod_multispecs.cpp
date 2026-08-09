/*
 * Configurable dual and triple specialization for AzerothCore 3.3.5a.
 */

#include "Chat.h"
#include "Config.h"
#include "DatabaseEnv.h"
#include "DBCStores.h"
#include "Log.h"
#include "Player.h"
#include "ScriptMgr.h"
#include "SpellDefines.h"

#include <algorithm>
#include <unordered_set>

using namespace Acore::ChatCommands;

namespace
{
struct MultispecsConfig
{
    bool enabled = true;
    uint8 dualSpecLevel = 10;
    uint8 tripleSpecLevel = 10;
    uint32 dualSpecPriceGold = 50;

    void Load()
    {
        enabled = sConfigMgr->GetOption<bool>("Multispecs.Enable", true);
        dualSpecLevel = std::max<uint8>(1, sConfigMgr->GetOption<uint8>("Multispecs.DualSpecLevel", 10));
        dualSpecPriceGold = sConfigMgr->GetOption<uint32>("Multispecs.DualSpecPriceGold", 50);
        tripleSpecLevel = std::max<uint8>(dualSpecLevel,
            sConfigMgr->GetOption<uint8>("Multispecs.TripleSpecLevel", 10));
    }
};

MultispecsConfig config;

bool HasDualSpecPurchase(Player const* player)
{
    if (!player)
        return false;

    QueryResult result = CharacterDatabase.Query(
        "SELECT `dual_spec` FROM `character_multispec_unlock` WHERE `guid` = {}",
        player->GetGUID().GetCounter());
    return result && result->Fetch()[0].Get<uint8>() != 0;
}

bool HasTripleSpecEntitlement(Player const* player)
{
    if (!player)
        return false;

    QueryResult result = CharacterDatabase.Query(
        "SELECT `triple_spec` FROM `character_multispec_entitlement` WHERE `guid` = {}",
        player->GetGUID().GetCounter());
    return result && result->Fetch()[0].Get<uint8>() != 0;
}

uint32 GetDualSpecPriceCopper()
{
    return static_cast<uint32>(std::min<uint64>(uint64(config.dualSpecPriceGold) * GOLD,
        MAX_MONEY_AMOUNT));
}

void SendThirdSpecTalents(Player* player)
{
    ChatHandler chat(player->GetSession());
    chat.PSendSysMessage("MULTISPECS_TALENTS_BEGIN 3 {}", player->GetActiveSpec() == 2 ?
        player->GetFreeTalentPoints() : 0);

    std::string payload;
    for (auto const& [spellId, talent] : player->GetTalentMap())
    {
        if (!talent || talent->State == PLAYERSPELL_REMOVED || !(talent->specMask & (1 << 2)))
            continue;

        TalentSpellPos const* pos = GetTalentSpellPos(spellId);
        TalentEntry const* entry = pos ? sTalentStore.LookupEntry(pos->talent_id) : nullptr;
        TalentTabEntry const* tab = entry ? sTalentTabStore.LookupEntry(entry->TalentTab) : nullptr;
        if (!entry || !tab || !(tab->ClassMask & player->getClassMask()))
            continue;

        std::string item = Acore::StringFormat("{},{},{},{};", tab->tabpage + 1,
            entry->Row + 1, entry->Col + 1, pos->rank + 1);
        if (payload.size() + item.size() > 180)
        {
            chat.PSendSysMessage("MULTISPECS_TALENTS_DATA {}", payload);
            payload.clear();
        }
        payload += item;
    }
    if (!payload.empty())
        chat.PSendSysMessage("MULTISPECS_TALENTS_DATA {}", payload);
    chat.SendSysMessage("MULTISPECS_TALENTS_END 3");
}

uint8 GetUnlockedSpecCount(Player const* player)
{
    if (!config.enabled || !player)
        return 1;

    bool dualPurchased = HasDualSpecPurchase(player);
    if (!dualPurchased || player->GetLevel() < config.dualSpecLevel)
        return 1;
    if (player->GetLevel() >= config.tripleSpecLevel && HasTripleSpecEntitlement(player))
        return 3;
    return 2;
}

void SendState(Player* player)
{
    bool dualPurchased = HasDualSpecPurchase(player);
    bool tripleEntitled = HasTripleSpecEntitlement(player);
    ChatHandler(player->GetSession()).PSendSysMessage("MULTISPECS_STATE {} {} {} {} {} {} {}",
        player->GetActiveSpec() + 1, GetUnlockedSpecCount(player), config.dualSpecLevel,
        config.tripleSpecLevel, config.dualSpecPriceGold, dualPurchased ? 1 : 0,
        tripleEntitled ? 1 : 0);
}

void ApplyUnlocks(Player* player)
{
    uint8 unlocked = GetUnlockedSpecCount(player);
    if (!player)
        return;
    if (player->GetActiveSpec() >= unlocked)
        player->ActivateSpec(0);
    if (player->GetSpecsCount() < unlocked)
        player->UpdateSpecCount(unlocked);
}

bool CanSwitch(Player const* player)
{
    return player && player->IsAlive() && !player->IsInCombat() && !player->IsInFlight() &&
        !player->IsBeingTeleported();
}

class MultispecsWorldScript final : public WorldScript
{
public:
    MultispecsWorldScript() : WorldScript("MultispecsWorldScript", { WORLDHOOK_ON_BEFORE_CONFIG_LOAD }) { }

    void OnBeforeConfigLoad(bool /*reload*/) override
    {
        config.Load();
    }
};

class MultispecsPlayerScript final : public PlayerScript
{
public:
    MultispecsPlayerScript() : PlayerScript("MultispecsPlayerScript",
        { PLAYERHOOK_ON_LOGIN, PLAYERHOOK_ON_LEVEL_CHANGED,
            PLAYERHOOK_ON_AFTER_SPEC_SLOT_CHANGED,
            PLAYERHOOK_ON_PLAYER_LEARN_TALENTS, PLAYERHOOK_ON_TALENTS_RESET,
            PLAYERHOOK_ON_UPDATE }) { }

    void OnPlayerLogin(Player* player) override
    {
        ApplyUnlocks(player);
    }

    void OnPlayerLevelChanged(Player* player, uint8 /*oldLevel*/) override
    {
        ApplyUnlocks(player);
    }

    void OnPlayerAfterSpecSlotChanged(Player* player, uint8 /*newSlot*/) override
    {
        SendState(player);
        SendThirdSpecTalents(player);
    }

    void OnPlayerLearnTalents(Player* player, uint32 /*talentId*/, uint32 /*talentRank*/,
        uint32 /*spellId*/) override
    {
        if (player->GetActiveSpec() >= 2)
            SendThirdSpecTalents(player);
    }

    void OnPlayerTalentsReset(Player* player, bool /*noCost*/) override
    {
        // This hook runs immediately before Player::resetTalents mutates the
        // talent map. Defer the snapshot until the next player update so a
        // trainer reset cannot leave the virtual third-spec UI stale.
        if (player->GetActiveSpec() >= 2)
            pendingTalentRefreshes.insert(player->GetGUID().GetCounter());
    }

    void OnPlayerUpdate(Player* player, uint32 /*diff*/) override
    {
        if (pendingTalentRefreshes.erase(player->GetGUID().GetCounter()) != 0)
            SendThirdSpecTalents(player);
    }

private:
    std::unordered_set<uint32> pendingTalentRefreshes;
};

class MultispecsCommandScript final : public CommandScript
{
public:
    MultispecsCommandScript() : CommandScript("MultispecsCommandScript") { }

    ChatCommandTable GetCommands() const override
    {
        static ChatCommandTable multispecsCommands =
        {
            { "switch", HandleSwitch, rbac::RBAC_PERM_COMMAND_SERVER_INFO, Console::No },
            { "buydual", HandleBuyDual, rbac::RBAC_PERM_COMMAND_SERVER_INFO, Console::No },
            { "buytriple", HandleBuyTriple, rbac::RBAC_PERM_COMMAND_SERVER_INFO, Console::No },
            { "status", HandleStatus, rbac::RBAC_PERM_COMMAND_SERVER_INFO, Console::No }
        };
        static ChatCommandTable commands =
        {
            { "multispec", multispecsCommands }
        };
        return commands;
    }

private:
    static bool HandleSwitch(ChatHandler* handler, uint8 displayIndex)
    {
        Player* player = handler->GetSession()->GetPlayer();
        ApplyUnlocks(player);
        uint8 unlocked = GetUnlockedSpecCount(player);
        if (displayIndex < 1 || displayIndex > unlocked || displayIndex > player->GetSpecsCount())
        {
            handler->PSendSysMessage("That specialization is locked. Dual spec costs {} gold at level {}; triple spec requires level {}, dual spec, and the character-bound website-shop perk.",
                config.dualSpecPriceGold, config.dualSpecLevel, config.tripleSpecLevel);
            return false;
        }

        if (!CanSwitch(player))
        {
            handler->SendSysMessage("You must be alive, out of combat, and standing still in the world to switch specs.");
            return false;
        }

        if (player->GetActiveSpec() + 1 == displayIndex)
        {
            HandleStatus(handler);
            return true;
        }

        // Reuse the client's native five-second specialization activation spell,
        // overriding its effect value so the core selects talent group three.
        CustomSpellValues values;
        values.AddSpellMod(SPELLVALUE_BASE_POINT0, displayIndex);
        return player->CastCustomSpell(63645, values, player, TRIGGERED_NONE) == SPELL_CAST_OK;
    }

    static bool HandleStatus(ChatHandler* handler)
    {
        Player* player = handler->GetSession()->GetPlayer();
        ApplyUnlocks(player);
        SendState(player);
        SendThirdSpecTalents(player);
        return true;
    }

    static bool HandleBuyDual(ChatHandler* handler)
    {
        Player* player = handler->GetSession()->GetPlayer();
        if (!config.enabled)
        {
            handler->SendSysMessage("Multiple specializations are disabled.");
            return false;
        }
        if (HasDualSpecPurchase(player))
        {
            handler->SendSysMessage("You have already purchased dual specialization.");
            ApplyUnlocks(player);
            SendState(player);
            return true;
        }
        if (player->GetLevel() < config.dualSpecLevel)
        {
            handler->PSendSysMessage("You must reach level {} to purchase dual specialization.",
                config.dualSpecLevel);
            return false;
        }

        uint32 price = GetDualSpecPriceCopper();
        if (!player->HasEnoughMoney(price))
        {
            handler->PSendSysMessage("Dual specialization costs {} gold.", config.dualSpecPriceGold);
            return false;
        }

        CharacterDatabase.DirectExecute(
            "INSERT INTO `character_multispec_unlock` (`guid`, `dual_spec`, `purchased_at`) "
            "VALUES ({}, 1, NOW()) ON DUPLICATE KEY UPDATE `dual_spec` = 1, `purchased_at` = NOW()",
            player->GetGUID().GetCounter());
        if (!HasDualSpecPurchase(player))
        {
            LOG_ERROR("module", "mod-multispecs: failed to persist dual-spec purchase for {} (GUID {})",
                player->GetName(), player->GetGUID().GetCounter());
            handler->SendSysMessage("The dual-specialization purchase could not be saved. You were not charged.");
            return false;
        }
        if (price)
            player->ModifyMoney(-static_cast<int32>(price));

        LOG_INFO("module", "mod-multispecs: {} (GUID {}) purchased dual spec for {} gold",
            player->GetName(), player->GetGUID().GetCounter(), config.dualSpecPriceGold);
        ApplyUnlocks(player);
        handler->SendSysMessage("Dual specialization purchased.");
        SendState(player);
        return true;
    }

    static bool HandleBuyTriple(ChatHandler* handler)
    {
        Player* player = handler->GetSession()->GetPlayer();
        if (!HasDualSpecPurchase(player))
            handler->PSendSysMessage("Purchase dual specialization first for {} gold at level {}.",
                config.dualSpecPriceGold, config.dualSpecLevel);
        else if (player->GetLevel() < config.tripleSpecLevel)
            handler->PSendSysMessage("You must reach level {} to use triple specialization.",
                config.tripleSpecLevel);
        else if (!HasTripleSpecEntitlement(player))
            handler->SendSysMessage("Triple specialization is a character-bound perk available from the website shop.");
        else
        {
            ApplyUnlocks(player);
            handler->SendSysMessage("This character already owns triple specialization.");
        }
        SendState(player);
        return true;
    }
};
}

void Addmod_multispecsScripts()
{
    new MultispecsWorldScript();
    new MultispecsPlayerScript();
    new MultispecsCommandScript();
}
