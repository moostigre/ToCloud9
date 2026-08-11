-- Grandfather characters that owned native/automatic dual specialization
-- before mod-multispecs introduced its character purchase table.
INSERT INTO `character_multispec_unlock` (`guid`, `dual_spec`, `purchased_at`)
SELECT `guid`, 1, NULL
FROM `characters`
WHERE `talentGroupsCount` >= 2
ON DUPLICATE KEY UPDATE `dual_spec` = GREATEST(`dual_spec`, 1);
