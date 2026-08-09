#include "Config.h"
#include "DatabaseEnv.h"
#include "ItemTemplate.h"
#include "Log.h"
#include "ObjectMgr.h"
#include "ScriptMgr.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <stdexcept>
#include <string>
#include <unordered_map>
#include <vector>

namespace
{
constexpr uint8 MaxItemStats = MAX_ITEM_PROTO_STATS;
constexpr uint8 MaxItemSpells = MAX_ITEM_PROTO_SPELLS;

struct ItemStatOverride
{
    uint32 entry = 0;
    uint8 statsCount = 0;
    std::array<_ItemStat, MaxItemStats> stats{};
    std::array<bool, MaxItemStats> present{};
    std::array<_Spell, MaxItemSpells> spells{};
    std::array<bool, MaxItemSpells> spellPresent{};
};

struct RetroItemizationConfig
{
    bool enabled = false;
    bool strictData = true;
    std::string profile = "wotlk";

    void Load()
    {
        enabled = sConfigMgr->GetOption<bool>("RetroItemization.Enable", false);
        strictData = sConfigMgr->GetOption<bool>("RetroItemization.StrictData", true);
        profile = sConfigMgr->GetOption<std::string>("RetroItemization.Profile", "WotLK");
        std::transform(profile.begin(), profile.end(), profile.begin(),
            [](unsigned char character) { return static_cast<char>(std::tolower(character)); });

        if (profile != "vanilla" && profile != "tbc" && profile != "wotlk")
            throw std::runtime_error("RetroItemization.Profile must be Vanilla, TBC, or WotLK");
    }
};

RetroItemizationConfig config;

void DataError(std::string const& message)
{
    if (config.strictData)
        throw std::runtime_error("mod-retro-itemization: " + message);

    LOG_ERROR("module", "mod-retro-itemization: {}", message);
}

std::unordered_map<uint32, ItemStatOverride> LoadOverrides()
{
    std::unordered_map<uint32, ItemStatOverride> overrides;
    QueryResult profileRow = WorldDatabase.Query(
        "SELECT `expected_items` FROM `retro_itemization_profile` WHERE `profile` = '{}'", config.profile);
    if (!profileRow)
    {
        DataError("selected profile '" + config.profile + "' has no import metadata");
        return overrides;
    }
    uint32 expectedItems = profileRow->Fetch()[0].Get<uint32>();

    QueryResult itemRows = WorldDatabase.Query(
        "SELECT `entry`, `stats_count` FROM `retro_itemization_item` "
        "WHERE `profile` = '{}' ORDER BY `entry`", config.profile);

    if (!itemRows)
    {
        DataError("selected profile '" + config.profile + "' contains no item rows");
        return overrides;
    }

    do
    {
        Field* fields = itemRows->Fetch();
        uint32 entry = fields[0].Get<uint32>();
        uint8 statsCount = fields[1].Get<uint8>();
        if (statsCount > MaxItemStats)
        {
            DataError("item " + std::to_string(entry) + " has stats_count above 10");
            continue;
        }

        overrides.emplace(entry, ItemStatOverride{entry, statsCount});
    } while (itemRows->NextRow());

    if (overrides.size() != expectedItems)
        DataError("profile metadata expects " + std::to_string(expectedItems) + " items but contains " +
            std::to_string(overrides.size()));

    QueryResult statRows = WorldDatabase.Query(
        "SELECT `entry`, `stat_slot`, `stat_type`, `stat_value` FROM `retro_itemization_stat` "
        "WHERE `profile` = '{}' ORDER BY `entry`, `stat_slot`", config.profile);

    if (statRows)
    {
        do
        {
            Field* fields = statRows->Fetch();
            uint32 entry = fields[0].Get<uint32>();
            uint8 slot = fields[1].Get<uint8>();
            uint32 statType = fields[2].Get<uint32>();
            int32 statValue = fields[3].Get<int32>();
            auto item = overrides.find(entry);

            if (item == overrides.end())
            {
                DataError("stat row references undeclared item " + std::to_string(entry));
                continue;
            }
            if (!slot || slot > MaxItemStats || slot > item->second.statsCount)
            {
                DataError("item " + std::to_string(entry) + " has invalid stat slot " + std::to_string(slot));
                continue;
            }
            if (statType >= MAX_ITEM_MOD)
            {
                DataError("item " + std::to_string(entry) + " has invalid stat type " +
                    std::to_string(statType));
                continue;
            }

            std::size_t index = slot - 1;
            item->second.stats[index].ItemStatType = statType;
            item->second.stats[index].ItemStatValue = statValue;
            item->second.present[index] = true;
        } while (statRows->NextRow());
    }

    QueryResult spellRows = WorldDatabase.Query(
        "SELECT `entry`, `spell_slot`, `spell_id`, `spell_trigger`, `spell_charges`, "
        "`spell_ppm_rate`, `spell_cooldown`, `spell_category`, `spell_category_cooldown` "
        "FROM `retro_itemization_spell` WHERE `profile` = '{}' ORDER BY `entry`, `spell_slot`",
        config.profile);

    if (spellRows)
    {
        do
        {
            Field* fields = spellRows->Fetch();
            uint32 entry = fields[0].Get<uint32>();
            uint8 slot = fields[1].Get<uint8>();
            auto item = overrides.find(entry);

            if (item == overrides.end())
            {
                DataError("spell row references undeclared item " + std::to_string(entry));
                continue;
            }
            if (!slot || slot > MaxItemSpells)
            {
                DataError("item " + std::to_string(entry) + " has invalid spell slot " +
                    std::to_string(slot));
                continue;
            }

            std::size_t index = slot - 1;
            _Spell& spell = item->second.spells[index];
            spell.SpellId = fields[2].Get<int32>();
            spell.SpellTrigger = fields[3].Get<uint32>();
            spell.SpellCharges = fields[4].Get<int32>();
            spell.SpellPPMRate = fields[5].Get<float>();
            spell.SpellCooldown = fields[6].Get<int32>();
            spell.SpellCategory = fields[7].Get<uint32>();
            spell.SpellCategoryCooldown = fields[8].Get<int32>();
            item->second.spellPresent[index] = true;
        } while (spellRows->NextRow());
    }

    for (auto const& [entry, item] : overrides)
    {
        for (uint8 index = 0; index < item.statsCount; ++index)
            if (!item.present[index])
                DataError("item " + std::to_string(entry) + " is missing stat slot " +
                    std::to_string(index + 1));
    }

    return overrides;
}

void ApplyOverrides()
{
    if (!config.enabled || config.profile == "wotlk")
        return;

    std::unordered_map<uint32, ItemStatOverride> overrides = LoadOverrides();
    if (overrides.empty())
        return;

    std::unordered_map<uint32, ItemTemplate*> templates;
    for (ItemTemplate* item : *sObjectMgr->GetItemTemplateStoreFast())
        if (item)
            templates.emplace(item->ItemId, item);

    std::vector<uint32> missingItems;
    for (auto const& [entry, override] : overrides)
        if (!templates.contains(entry))
            missingItems.push_back(entry);

    if (!missingItems.empty())
    {
        DataError("profile references " + std::to_string(missingItems.size()) +
            " items absent from the WotLK item template store");
        if (config.strictData)
            return;
    }

    uint32 applied = 0;
    for (auto const& [entry, override] : overrides)
    {
        auto target = templates.find(entry);
        if (target == templates.end())
            continue;

        target->second->StatsCount = override.statsCount;
        std::copy(override.stats.begin(), override.stats.end(), target->second->ItemStat);
        for (uint8 index = 0; index < MaxItemSpells; ++index)
            if (override.spellPresent[index])
                target->second->Spells[index] = override.spells[index];
        ++applied;
    }

    LOG_INFO("module", "mod-retro-itemization: applied '{}' stat-and-spell profile to {} items",
        config.profile, applied);
}

class RetroItemizationWorldScript final : public WorldScript
{
public:
    RetroItemizationWorldScript() : WorldScript("RetroItemizationWorldScript",
    {
        WORLDHOOK_ON_BEFORE_CONFIG_LOAD,
        WORLDHOOK_ON_BEFORE_WORLD_INITIALIZED
    }) { }

    void OnBeforeConfigLoad(bool reload) override
    {
        config.Load();
        LOG_INFO("module", "mod-retro-itemization: enabled={}, profile='{}', strict-data={}{}",
            config.enabled, config.profile, config.strictData,
            reload ? " (profile changes require a worldserver restart)" : "");
    }

    void OnBeforeWorldInitialized() override
    {
        ApplyOverrides();
    }
};
}

void Addmod_retro_itemizationScripts()
{
    new RetroItemizationWorldScript();
}
