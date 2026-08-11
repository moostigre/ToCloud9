-- Character-specific in-game purchases.
CREATE TABLE IF NOT EXISTS `character_multispec_unlock` (
  `guid` INT UNSIGNED NOT NULL,
  `dual_spec` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `purchased_at` TIMESTAMP NULL DEFAULT NULL,
  PRIMARY KEY (`guid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Character-bound perks granted by the website shop.
CREATE TABLE IF NOT EXISTS `character_multispec_entitlement` (
  `guid` INT UNSIGNED NOT NULL,
  `triple_spec` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `granted_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `source` VARCHAR(64) NOT NULL DEFAULT 'website-shop',
  PRIMARY KEY (`guid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Compatibility for deployed builds where triple specialization was
-- account-bound. Keep this table while those binaries remain in rotation.
CREATE TABLE IF NOT EXISTS `account_multispec_entitlement` (
  `account_id` INT UNSIGNED NOT NULL,
  `triple_spec` TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `granted_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `source` VARCHAR(64) NOT NULL DEFAULT 'website-shop',
  PRIMARY KEY (`account_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Preserve dual specialization for characters created before purchases became
-- character-bound. The stored talent-group count is authoritative evidence
-- that the character already owned the second slot.
INSERT INTO `character_multispec_unlock` (`guid`, `dual_spec`, `purchased_at`)
SELECT `guid`, 1, NULL
FROM `characters`
WHERE `talentGroupsCount` >= 2
ON DUPLICATE KEY UPDATE `dual_spec` = GREATEST(`dual_spec`, 1);
