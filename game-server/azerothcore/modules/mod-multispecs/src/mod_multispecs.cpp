/*
 * Configurable dual and triple specialization for AzerothCore 3.3.5a.
 */

#include "Chat.h"
#include "Config.h"
#include "DBCStores.h"
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
    uint8 tripleSpecLevel = 40;

    void Load()
    {
        enabled = sConfigMgr->GetOption<bool>("Multispecs.Enable", true);
        dualSpecLevel = std::max<uint8>(1, sConfigMgr->GetOption<uint8>("Multispecs.DualSpecLevel", 10));
        tripleSpecLevel = std::max<uint8>(dualSpecLevel,
            sConfigMgr->GetOption<uint8>("Multispecs.TripleSpecLevel", 40));
    }
};

MultispecsConfig config;

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
    if (player->GetLevel() >= config.tripleSpecLevel)
        return 3;
    if (player->GetLevel() >= config.dualSpecLevel)
        return 2;
    return 1;
}

void ApplyUnlocks(Player* player)
{
    uint8 unlocked = GetUnlockedSpecCount(player);
    if (player && player->GetSpecsCount() < unlocked)
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
        ChatHandler(player->GetSession()).PSendSysMessage("MULTISPECS_STATE {} {} {} {}",
            player->GetActiveSpec() + 1, GetUnlockedSpecCount(player),
            config.dualSpecLevel, config.tripleSpecLevel);
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
            { "reset", HandleReset, rbac::RBAC_PERM_COMMAND_SERVER_INFO, Console::No },
            { "status", HandleStatus, rbac::RBAC_PERM_COMMAND_SERVER_INFO, Console::No }
        };
        static ChatCommandTable commands =
        {
            { "multispec", multispecsCommands }
        };
        return commands;
    }

private:
    static bool HandleReset(ChatHandler* handler, uint8 displayIndex)
    {
        Player* player = handler->GetSession()->GetPlayer();
        if (displayIndex != 3 || player->GetActiveSpec() + 1 != displayIndex)
        {
            handler->SendSysMessage("Only the active third specialization can be reset with this control.");
            return false;
        }

        if (!CanSwitch(player))
        {
            handler->SendSysMessage("You must be alive, out of combat, and standing still in the world to reset talents.");
            return false;
        }

        if (!player->resetTalents(false))
        {
            handler->SendSysMessage("That specialization has no talents to reset, or you cannot afford the reset cost.");
            SendThirdSpecTalents(player);
            return false;
        }

        player->SendTalentsInfoData(false);
        SendThirdSpecTalents(player);
        handler->SendSysMessage("Your third specialization talents have been reset.");
        return true;
    }

    static bool HandleSwitch(ChatHandler* handler, uint8 displayIndex)
    {
        Player* player = handler->GetSession()->GetPlayer();
        ApplyUnlocks(player);
        uint8 unlocked = GetUnlockedSpecCount(player);
        if (displayIndex < 1 || displayIndex > unlocked || displayIndex > player->GetSpecsCount())
        {
            handler->PSendSysMessage("That specialization is locked. Dual spec unlocks at level {} and triple spec at level {}.",
                config.dualSpecLevel, config.tripleSpecLevel);
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
        handler->PSendSysMessage("MULTISPECS_STATE {} {} {} {}", player->GetActiveSpec() + 1,
            GetUnlockedSpecCount(player), config.dualSpecLevel, config.tripleSpecLevel);
        SendThirdSpecTalents(player);
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
