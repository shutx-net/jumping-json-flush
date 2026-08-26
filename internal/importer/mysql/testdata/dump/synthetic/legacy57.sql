--
-- HAND-WRITTEN FIXTURE. This file was NOT produced by mysqldump, and it is not
-- a capture of MySQL 5.7: no 5.7 server is installed anywhere this repository
-- is built, and a file claiming to be a capture of one would be a lie that
-- looks like data. Every real capture lives under testdata/dump/mysql<series>/.
--
-- What it exercises are the shapes MySQL 5.7 wrote, and that hand-edited files
-- still carry:
--
--   * a server-version banner naming a server OUTSIDE the range Import was
--     written against, which is the only way the version warning gets an input.
--     The words of that banner are deliberately not repeated in this header:
--     serverVersion takes the FIRST comment line that carries them, so a
--     prose mention above the real one would shadow it;
--   * integer display widths - "int(11)", "bigint(20) unsigned" - which MySQL
--     deprecated in 8.0.17 and mysqldump stopped writing in 8.0.19, so no
--     capture can contain one;
--   * "DEFAULT CHARSET=utf8" with no COLLATE= beside it, which is what 5.7
--     wrote before utf8mb4 became the server default;
--   * a bare "KEY" element with no index type after the column list.
--
-- The table it defines is otherwise ordinary, so the document it produces is
-- one internal/check has nothing to say about.
--

-- MySQL dump 10.13  Distrib 5.7.44, for Linux (x86_64)
--
-- Host: localhost    Database: legacy
-- ------------------------------------------------------
-- Server version	5.7.44-log

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET NAMES utf8 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;

--
-- Table structure for table `members`
--

DROP TABLE IF EXISTS `members`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!40101 SET character_set_client = utf8 */;
CREATE TABLE `members` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `email` varchar(255) NOT NULL COMMENT 'メールアドレス',
  `points` int(11) NOT NULL DEFAULT '0',
  `joined_on` date DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_members_email` (`email`),
  KEY `ix_members_joined_on` (`joined_on`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COMMENT='会員';
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;

-- Dump completed on 2019-11-01  9:14:52
