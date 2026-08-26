--
-- HAND-WRITTEN FIXTURE: a dump whose second column list is never closed. It was
-- NOT produced by mysqldump, and it is not a capture of any server version:
-- every directory under testdata/dump/mysql*/ is a real capture, and none of
-- them writes the file below.
--
-- It carries no version banner for that reason, which also covers the case
-- Import treats as legitimate on purpose - a hand-edited or concatenated dump
-- has nothing to version check.
--
-- The first table is valid so that the reported line number is not 1.
--

DROP TABLE IF EXISTS `authors`;
CREATE TABLE `authors` (
  `id` int NOT NULL AUTO_INCREMENT,
  `name` varchar(120) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

DROP TABLE IF EXISTS `books`;
CREATE TABLE `books` (
  `id` int NOT NULL,
  `title` varchar(200) NOT NULL
