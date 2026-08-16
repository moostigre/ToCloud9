CREATE TABLE IF NOT EXISTS `mod_qol_character_tiny_mount` (
  `guid` INT UNSIGNED NOT NULL,
  `spell` INT UNSIGNED NOT NULL,
  PRIMARY KEY (`guid`, `spell`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

DELETE `cs`
FROM `character_spell` AS `cs`
JOIN `mod_qol_character_tiny_mount` AS `tm`
  ON `tm`.`guid` = `cs`.`guid`
 AND `tm`.`spell` = `cs`.`spell`;

DELETE `ci`
FROM `character_inventory` AS `ci`
JOIN `item_instance` AS `ii`
  ON `ii`.`guid` = `ci`.`item`
WHERE `ii`.`itemEntry` BETWEEN 900000 AND 999999;

DELETE `mi`
FROM `mail_items` AS `mi`
JOIN `item_instance` AS `ii`
  ON `ii`.`guid` = `mi`.`item_guid`
WHERE `ii`.`itemEntry` BETWEEN 900000 AND 999999;

DELETE FROM `item_instance`
WHERE `itemEntry` BETWEEN 900000 AND 999999;

DROP TABLE IF EXISTS `mod_qol_character_tiny_mount`;
