--
-- HAND-WRITTEN FIXTURE: a dump whose second column list is never closed. It
-- was NOT produced by mysqldump, which is why it carries no banner.
--
-- The first table is valid so that the reported line number is not 1.
--

DROP TABLE IF EXISTS `authors`;
CREATE TABLE `authors` (
  `id` int NOT NULL,
  `name` varchar(120) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

DROP TABLE IF EXISTS `books`;
CREATE TABLE `books` (
  `id` int NOT NULL,
  `title` varchar(200) NOT NULL
