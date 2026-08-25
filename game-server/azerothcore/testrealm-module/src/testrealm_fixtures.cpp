/*
 * Private test-realm fixture support. This module is baked only into the
 * immutable ephemeral-realm images and the command refuses in-game sessions.
 */

#include "AccountMgr.h"
#include "CharacterCache.h"
#include "Chat.h"
#include "CommandScript.h"
#include "DatabaseEnv.h"
#include "Item.h"
#include "Mail.h"
#include "MotionMaster.h"
#include "ObjectMgr.h"
#include "Player.h"
#include "QuestDef.h"
#include "RBAC.h"
#include "Realm.h"
#include "ReputationMgr.h"
#include "ScriptMgr.h"
#include "SharedDefines.h"
#include "WorldSession.h"

#include <algorithm>
#include <charconv>
#include <limits>
#include <memory>
#include <sstream>
#include <string>
#include <string_view>
#include <unordered_set>
#include <utility>
#include <vector>

using namespace Acore::ChatCommands;

namespace
{
constexpr uint32 MaxFixtureItems = 40;
constexpr uint32 MaxFixtureItemCount = 200;
constexpr uint32 MaxFixtureAttachments = 120;
constexpr uint32 MaxFixtureQuests = 25;

struct FixtureItem
{
    uint32 Id;
    uint32 Count;
};

class FixtureCreateInfo final : public CharacterCreateInfo
{
public:
    FixtureCreateInfo(std::string name, uint8 race, uint8 playerClass)
    {
        Name = std::move(name);
        Race = race;
        Class = playerClass;
        Gender = GENDER_MALE;
        OutfitId = 0;
    }
};

template <typename T>
bool ParseUnsigned(std::string_view value, T& output)
{
    if (value.empty())
        return false;

    uint64 parsed = 0;
    auto const result = std::from_chars(value.data(), value.data() + value.size(), parsed);
    if (result.ec != std::errc() || result.ptr != value.data() + value.size() || parsed > std::numeric_limits<T>::max())
        return false;

    output = static_cast<T>(parsed);
    return true;
}

std::vector<std::string> Split(std::string const& value, char separator)
{
    std::vector<std::string> parts;
    std::stringstream stream(value);
    std::string part;
    while (std::getline(stream, part, separator))
        parts.push_back(part);
    return parts;
}

bool ParseItems(std::string const& value, std::vector<FixtureItem>& items)
{
    if (value == "-")
        return true;

    std::unordered_set<uint32> seen;
    for (std::string const& token : Split(value, ','))
    {
        std::vector<std::string> fields = Split(token, ':');
        FixtureItem item{};
        if (fields.size() != 2 || !ParseUnsigned(fields[0], item.Id) || !ParseUnsigned(fields[1], item.Count) ||
            !item.Id || !item.Count || item.Count > MaxFixtureItemCount || !seen.insert(item.Id).second)
            return false;
        items.push_back(item);
    }
    return items.size() <= MaxFixtureItems;
}

bool ParseQuests(std::string const& value, std::vector<uint32>& quests, uint32 limit, std::unordered_set<uint32>& seen)
{
    if (value == "-")
        return true;

    for (std::string const& token : Split(value, ','))
    {
        uint32 quest = 0;
        if (!ParseUnsigned(token, quest) || !quest || !seen.insert(quest).second)
            return false;
        quests.push_back(quest);
    }
    return quests.size() <= limit;
}

bool Fail(ChatHandler* handler, std::string const& message)
{
    handler->SendSysMessage(("TC9_FIXTURE_ERROR " + message).c_str());
    return false;
}

bool SendFixtureItems(Player* player, std::vector<FixtureItem> const& requested)
{
    std::vector<FixtureItem> attachments;
    for (FixtureItem const& request : requested)
    {
        ItemTemplate const* itemTemplate = sObjectMgr->GetItemTemplate(request.Id);
        if (!itemTemplate || (itemTemplate->MaxCount > 0 && request.Count > static_cast<uint32>(itemTemplate->MaxCount)))
            return false;

        uint32 remaining = request.Count;
        uint32 maxStack = std::max<uint32>(1, itemTemplate->GetMaxStackSize());
        while (remaining)
        {
            uint32 count = std::min(remaining, maxStack);
            attachments.push_back({ request.Id, count });
            remaining -= count;
            if (attachments.size() > MaxFixtureAttachments)
                return false;
        }
    }

    if (attachments.empty())
        return true;

    CharacterDatabaseTransaction transaction = CharacterDatabase.BeginTransaction();
    MailSender sender(MAIL_NORMAL, player->GetGUID().GetCounter(), MAIL_STATIONERY_GM);
    for (std::size_t offset = 0; offset < attachments.size(); offset += MAX_MAIL_ITEMS)
    {
        MailDraft draft("Test realm fixtures", "Requested test items are attached.");
        std::size_t end = std::min(attachments.size(), offset + MAX_MAIL_ITEMS);
        for (std::size_t index = offset; index < end; ++index)
        {
            FixtureItem const& attachment = attachments[index];
            Item* item = Item::CreateItem(attachment.Id, attachment.Count, player);
            if (!item)
                return false;
            item->SaveToDB(transaction);
            draft.AddItem(item);
        }
        draft.SendMailTo(transaction, MailReceiver(player, player->GetGUID().GetCounter()), sender);
    }
    CharacterDatabase.CommitTransaction(transaction);
    return true;
}

bool ValidateFixtureItems(std::vector<FixtureItem> const& requested)
{
    uint32 attachmentCount = 0;
    for (FixtureItem const& request : requested)
    {
        ItemTemplate const* itemTemplate = sObjectMgr->GetItemTemplate(request.Id);
        if (!itemTemplate || (itemTemplate->MaxCount > 0 && request.Count > static_cast<uint32>(itemTemplate->MaxCount)))
            return false;
        attachmentCount += (request.Count + std::max<uint32>(1, itemTemplate->GetMaxStackSize()) - 1) / std::max<uint32>(1, itemTemplate->GetMaxStackSize());
        if (attachmentCount > MaxFixtureAttachments)
            return false;
    }
    return true;
}

bool CompleteFixtureQuest(Player* player, Quest const* quest)
{
    for (uint8 index = 0; index < QUEST_ITEM_OBJECTIVES_COUNT; ++index)
    {
        uint32 itemId = quest->RequiredItemId[index];
        uint32 requiredCount = quest->RequiredItemCount[index];
        uint32 currentCount = itemId ? player->GetItemCount(itemId, true) : 0;
        if (!itemId || currentCount >= requiredCount)
            continue;

        ItemPosCountVec destinations;
        uint32 missingCount = requiredCount - currentCount;
        if (player->CanStoreNewItem(NULL_BAG, NULL_SLOT, destinations, itemId, missingCount) != EQUIP_ERR_OK ||
            !player->StoreNewItem(destinations, itemId, true))
            return false;
    }

    // Mirror AzerothCore's .quest complete command so objective counters are
    // consistent with the completed quest-log state.
    for (uint8 index = 0; index < QUEST_OBJECTIVES_COUNT; ++index)
    {
        int32 creatureOrGameObject = quest->RequiredNpcOrGo[index];
        uint32 requiredCount = quest->RequiredNpcOrGoCount[index];
        if (creatureOrGameObject > 0)
        {
            CreatureTemplate const* creature = sObjectMgr->GetCreatureTemplate(creatureOrGameObject);
            if (!creature)
                return false;
            for (uint32 count = 0; count < requiredCount; ++count)
                player->KilledMonster(creature, ObjectGuid::Empty);
        }
        else if (creatureOrGameObject < 0)
            for (uint32 count = 0; count < requiredCount; ++count)
                player->KillCreditGO(creatureOrGameObject);
    }

    if (quest->HasSpecialFlag(QUEST_SPECIAL_FLAGS_PLAYER_KILL) && quest->GetPlayersSlain())
        player->KilledPlayerCreditForQuest(quest->GetPlayersSlain(), quest);

    auto satisfyReputation = [player](uint32 faction, uint32 requiredValue)
    {
        if (faction && player->GetReputationMgr().GetReputation(faction) < requiredValue)
            if (FactionEntry const* factionEntry = sFactionStore.LookupEntry(faction))
                player->GetReputationMgr().SetReputation(factionEntry, static_cast<float>(requiredValue));
    };
    satisfyReputation(quest->GetRepObjectiveFaction(), quest->GetRepObjectiveValue());
    satisfyReputation(quest->GetRepObjectiveFaction2(), quest->GetRepObjectiveValue2());

    int32 rewardOrRequiredMoney = quest->GetRewOrReqMoney(player->GetLevel());
    uint32 requiredMoney = rewardOrRequiredMoney < 0 ? static_cast<uint32>(-static_cast<int64>(rewardOrRequiredMoney)) : 0;
    if (requiredMoney && !player->HasEnoughMoney(requiredMoney))
        player->SetMoney(requiredMoney);

    player->CompleteQuest(quest->GetQuestId());
    return player->FindQuestSlot(quest->GetQuestId()) < MAX_QUEST_LOG_SIZE &&
        player->GetQuestStatus(quest->GetQuestId()) == QUEST_STATUS_COMPLETE &&
        !player->IsQuestRewarded(quest->GetQuestId()) && player->CanRewardQuest(quest, false);
}

class testrealm_fixture_commandscript : public CommandScript
{
public:
    testrealm_fixture_commandscript() : CommandScript("testrealm_fixture_commandscript") { }

    ChatCommandTable GetCommands() const override
    {
        static ChatCommandTable fixtureCommands =
        {
            { "fixture", HandleFixture, rbac::RBAC_PERM_COMMAND_ACCOUNT_CREATE, Console::Yes }
        };
        static ChatCommandTable commands =
        {
            { "testrealm", fixtureCommands }
        };
        return commands;
    }

    static bool HandleFixture(ChatHandler* handler, Tail arguments)
    {
        // This is an infrastructure-only command. Never expose it to an
        // authenticated game session, regardless of its RBAC permissions.
        if (handler->GetSession())
            return Fail(handler, "console-only command");

        std::istringstream input{ std::string(arguments) };
        uint32 accountId = 0;
        uint32 money = 0;
        uint8 race = 0;
        uint8 playerClass = 0;
        uint8 level = 0;
        std::string name;
        std::string accountToken, raceToken, classToken, levelToken, moneyToken, itemToken, activeToken, completedToken, trailing;
        if (!(input >> accountToken >> name >> raceToken >> classToken >> levelToken >> moneyToken >> itemToken >> activeToken >> completedToken) || input >> trailing ||
            !ParseUnsigned(accountToken, accountId) || !ParseUnsigned(raceToken, race) || !ParseUnsigned(classToken, playerClass) ||
            !ParseUnsigned(levelToken, level) || !ParseUnsigned(moneyToken, money) || !accountId || level < 1 || level > 80 || money > 10000U * GOLD)
            return Fail(handler, "invalid command arguments");

        if (!normalizePlayerName(name) || ObjectMgr::CheckPlayerName(name, true) != CHAR_NAME_SUCCESS || sCharacterCache->GetCharacterGuidByName(name))
            return Fail(handler, "invalid or duplicate character name");

        std::string accountName;
        if (!AccountMgr::GetName(accountId, accountName))
            return Fail(handler, "account does not exist");

        std::vector<FixtureItem> items;
        std::vector<uint32> activeQuests;
        std::vector<uint32> completedQuests;
        std::unordered_set<uint32> seenQuests;
        if (!ParseItems(itemToken, items) || !ParseQuests(activeToken, activeQuests, MaxFixtureQuests, seenQuests) ||
            !ParseQuests(completedToken, completedQuests, MaxFixtureQuests, seenQuests) ||
            activeQuests.size() + completedQuests.size() > MaxFixtureQuests)
            return Fail(handler, "invalid item or quest list");

        if (!ValidateFixtureItems(items))
            return Fail(handler, "unknown item ID or excessive generated stacks");
        for (uint32 quest : seenQuests)
        {
            Quest const* questTemplate = sObjectMgr->GetQuestTemplate(quest);
            if (!questTemplate)
                return Fail(handler, "unknown quest ID");
            if (questTemplate->HasFlag(QUEST_FLAGS_TRACKING))
                return Fail(handler, "tracking quests cannot remain in the quest log");
        }

        WorldSession fixtureSession(accountId, std::move(accountName), 0, nullptr, SEC_ADMINISTRATOR,
            EXPANSION_WRATH_OF_THE_LICH_KING, 0, LOCALE_enUS, 0, false, true, 0);
        FixtureCreateInfo createInfo(name, race, playerClass);
        std::shared_ptr<Player> player(new Player(&fixtureSession), [](Player* value)
        {
            if (value->HasAtLoginFlag(AT_LOGIN_FIRST))
                value->CleanupsBeforeDelete();
            delete value;
        });
        player->GetMotionMaster()->Initialize();
        if (!player->Create(sObjectMgr->GetGenerator<HighGuid::Player>().Generate(), &createInfo))
            return Fail(handler, "unsupported race/class combination");

        player->SetAtLoginFlag(AT_LOGIN_FIRST);
        if (level != player->GetLevel())
        {
            player->GiveLevel(level);
            player->InitTalentForLevel();
            player->SetUInt32Value(PLAYER_XP, 0);
        }
        player->SetMoney(money);
        for (uint32 quest : completedQuests)
        {
            Quest const* questTemplate = sObjectMgr->GetQuestTemplate(quest);
            player->AddQuest(questTemplate, nullptr);
            if (!CompleteFixtureQuest(player.get(), questTemplate))
                return Fail(handler, "quest could not be prepared for turn-in");
        }
        // Add incomplete quests last so completing another requested quest
        // cannot accidentally advance an overlapping active objective.
        for (uint32 quest : activeQuests)
        {
            player->AddQuest(sObjectMgr->GetQuestTemplate(quest), nullptr);
            if (player->FindQuestSlot(quest) >= MAX_QUEST_LOG_SIZE || player->GetQuestStatus(quest) != QUEST_STATUS_INCOMPLETE)
                return Fail(handler, "quest could not be added to the quest log");
        }

        CharacterDatabaseTransaction characterTransaction = CharacterDatabase.BeginTransaction();
        player->SaveToDB(characterTransaction, true, false);
        CharacterDatabase.CommitTransaction(characterTransaction);
        if (!SendFixtureItems(player.get(), items))
            return Fail(handler, "item count exceeds item or mail limits");

        sScriptMgr->OnPlayerCreate(player.get());
        sCharacterCache->AddCharacterCacheEntry(player->GetGUID(), accountId, player->GetName(), player->getGender(), player->getRace(), player->getClass(), player->GetLevel());

        uint8 characterCount = 1;
        CharacterDatabasePreparedStatement* characterCountStatement = CharacterDatabase.GetPreparedStatement(CHAR_SEL_SUM_CHARS);
        characterCountStatement->SetData(0, accountId);
        if (PreparedQueryResult result = CharacterDatabase.Query(characterCountStatement))
            characterCount = static_cast<uint8>((*result)[0].Get<uint64>());
        LoginDatabasePreparedStatement* countStatement = LoginDatabase.GetPreparedStatement(LOGIN_REP_REALM_CHARACTERS);
        countStatement->SetData(0, characterCount);
        countStatement->SetData(1, accountId);
        countStatement->SetData(2, realm.Id.Realm);
        LoginDatabase.Execute(countStatement);

        handler->SendSysMessage(("TC9_FIXTURE_OK " + player->GetName()).c_str());
        return true;
    }
};
}

void AddSC_testrealm_fixture_commandscript()
{
    new testrealm_fixture_commandscript();
}
