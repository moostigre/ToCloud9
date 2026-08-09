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
