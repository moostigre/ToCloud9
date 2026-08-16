-- Dual-specialization gossip price: 5 gold. AzerothCore uses BoxMoney for
-- both the displayed confirmation price and the amount charged.
UPDATE `gossip_menu_option`
SET `BoxMoney` = 50000
WHERE `OptionType` = 18;

DELETE FROM `spell_script_names`
WHERE `spell_id` = 8690 AND `ScriptName` = 'QolHearthstoneSpellScript';

INSERT INTO `spell_script_names` (`spell_id`, `ScriptName`)
VALUES (8690, 'QolHearthstoneSpellScript');
