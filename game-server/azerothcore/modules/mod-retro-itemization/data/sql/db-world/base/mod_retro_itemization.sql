CREATE TABLE IF NOT EXISTS `retro_itemization_profile` (
    `profile` VARCHAR(16) NOT NULL,
    `source_name` VARCHAR(255) NOT NULL,
    `source_build` INT UNSIGNED NOT NULL DEFAULT 0,
    `source_revision` VARCHAR(64) NOT NULL DEFAULT '',
    `expected_items` INT UNSIGNED NOT NULL,
    `generated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`profile`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `retro_itemization_item` (
    `profile` VARCHAR(16) NOT NULL,
    `entry` MEDIUMINT UNSIGNED NOT NULL,
    `stats_count` TINYINT UNSIGNED NOT NULL,
    PRIMARY KEY (`profile`, `entry`),
    CONSTRAINT `fk_retro_itemization_item_profile`
        FOREIGN KEY (`profile`) REFERENCES `retro_itemization_profile` (`profile`)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `retro_itemization_stat` (
    `profile` VARCHAR(16) NOT NULL,
    `entry` MEDIUMINT UNSIGNED NOT NULL,
    `stat_slot` TINYINT UNSIGNED NOT NULL,
    `stat_type` TINYINT UNSIGNED NOT NULL,
    `stat_value` SMALLINT NOT NULL,
    PRIMARY KEY (`profile`, `entry`, `stat_slot`),
    CONSTRAINT `fk_retro_itemization_stat_item`
        FOREIGN KEY (`profile`, `entry`) REFERENCES `retro_itemization_item` (`profile`, `entry`)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `retro_itemization_spell` (
    `profile` VARCHAR(16) NOT NULL,
    `entry` MEDIUMINT UNSIGNED NOT NULL,
    `spell_slot` TINYINT UNSIGNED NOT NULL,
    `spell_id` INT NOT NULL,
    `spell_trigger` TINYINT UNSIGNED NOT NULL,
    `spell_charges` SMALLINT NOT NULL,
    `spell_ppm_rate` FLOAT NOT NULL,
    `spell_cooldown` INT NOT NULL,
    `spell_category` SMALLINT UNSIGNED NOT NULL,
    `spell_category_cooldown` INT NOT NULL,
    PRIMARY KEY (`profile`, `entry`, `spell_slot`),
    CONSTRAINT `fk_retro_itemization_spell_item`
        FOREIGN KEY (`profile`, `entry`) REFERENCES `retro_itemization_item` (`profile`, `entry`)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
