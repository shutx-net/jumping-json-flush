-- Source DDL for the fixture that exercises every warning path.
--
-- Everything here is something the jjf design format cannot hold. Each one must
-- produce a warning and still leave a usable document behind: the goldens pin
-- the warnings and the document both, and internal/check must find nothing to
-- say about the result.
--
-- The one thing that is NOT a warning is here too, and is the reason the
-- comments below name it: ON UPDATE NO ACTION is written out and never comes
-- back, because mysqldump drops it exactly as pg_dump drops PostgreSQL's.

-- Created first because the load needs it, dumped last because mysqldump
-- orders tables by name. That is the point of it: in the capture the foreign
-- key in `order` points forward, at a table the parser has not read yet.
CREATE TABLE warehouses (
    id INT NOT NULL AUTO_INCREMENT,
    label VARCHAR(60) NOT NULL,
    PRIMARY KEY (id)
) ENGINE=InnoDB COMMENT='倉庫';

-- `order` is a reserved word, and so is `key`. MySQL takes both when they are
-- quoted, and a design document holds both without quoting anything, so this is
-- a case that has to work rather than a case that has to warn.
CREATE TABLE `order` (
    id INT NOT NULL AUTO_INCREMENT,
    `key` VARCHAR(40) NOT NULL COMMENT '注文キー
O''Brien の \\path も、改行も入る。',
    status ENUM('pending','paid','shipped') NOT NULL DEFAULT 'pending' COMMENT '注文状態',
    flags SET('gift','fragile'),
    price DECIMAL(10,2) NOT NULL,
    quantity INT NOT NULL DEFAULT 1,
    total DECIMAL(12,2) GENERATED ALWAYS AS (price * quantity) STORED,
    body TEXT,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    warehouse_id INT,
    PRIMARY KEY (id),
    UNIQUE KEY uq_order_key (`key`),
    KEY ix_order_body (body(64)),
    KEY ix_order_price (price DESC),
    FULLTEXT KEY ft_order_body (body),
    CONSTRAINT ck_order_quantity CHECK (quantity > 0),
    -- No index of its own, so InnoDB creates one and names it after this
    -- constraint. That is the collision a jjf document cannot hold.
    CONSTRAINT ct_order_warehouse FOREIGN KEY (warehouse_id) REFERENCES warehouses (id)
        ON DELETE SET DEFAULT ON UPDATE NO ACTION
) ENGINE=InnoDB COMMENT='注文
予約語で名付けた表。';

-- A non-InnoDB table. MyISAM silently ignores the foreign keys a document
-- would claim it has, which is why the engine is the one table option that
-- warns.
CREATE TABLE audit_log (
    id BIGINT NOT NULL AUTO_INCREMENT,
    happened_at DATETIME NOT NULL,
    payload JSON,
    PRIMARY KEY (id)
) ENGINE=MyISAM COMMENT='監査ログ';

-- A partitioned table. The partitioning is not the table the document
-- describes, but the columns still are, so the table is imported.
CREATE TABLE metrics (
    id BIGINT NOT NULL,
    day DATE NOT NULL,
    amount DOUBLE NOT NULL,
    PRIMARY KEY (id, day)
) ENGINE=InnoDB
PARTITION BY RANGE (YEAR(day)) (
    PARTITION p2023 VALUES LESS THAN (2024),
    PARTITION p2024 VALUES LESS THAN (2025),
    PARTITION pmax VALUES LESS THAN MAXVALUE
);
