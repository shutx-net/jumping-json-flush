-- Source DDL for the broad importer fixture.
--
-- It is loaded into a throwaway MySQL database by generate.sh; the dump of the
-- result is what the tests read. Everything here must import with ZERO
-- warnings, so that edge.sql is the only source of expected warning output.
--
-- Three rules keep it at zero, and each one is worth knowing before adding a
-- table here.
--
-- Every foreign key is covered by an index of its own, or by the leading
-- columns of the primary key. InnoDB creates an index for a foreign key that
-- has none and names it after the CONSTRAINT, and a jjf document keeps
-- constraint and index names in one namespace per table - so an uncovered
-- foreign key produces a warning. edge.sql has one on purpose.
--
-- Every DEFINER is written out as `jjf`@`%` rather than left to the server.
-- mysqldump writes the resolved definer of a view or a trigger, so a definer
-- the server filled in would make the capture depend on which account
-- generate.sh connected as.
--
-- No ENUM, no SET, no generated column, no prefix length and no
-- ON UPDATE CURRENT_TIMESTAMP: all of those are things the design format
-- cannot hold, and all of them belong in edge.sql.

CREATE TABLE customers (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    email VARCHAR(255) NOT NULL COMMENT 'メールアドレス',
    display_name VARCHAR(120) NOT NULL,
    note TEXT,
    referrer_id BIGINT UNSIGNED,
    profile JSON,
    active TINYINT(1) NOT NULL DEFAULT 1,
    born_on DATE,
    balance DECIMAL(10,2) NOT NULL DEFAULT 0 COMMENT '残高',
    registered_at DATETIME(3) NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_customers_email (email),
    KEY ix_customers_referrer (referrer_id),
    CONSTRAINT ct_customers_referrer FOREIGN KEY (referrer_id) REFERENCES customers (id) ON DELETE SET NULL
) ENGINE=InnoDB COMMENT='顧客
オンラインショップの利用者。退会しても行は残す。';

CREATE TABLE products (
    id INT NOT NULL AUTO_INCREMENT,
    sku CHAR(12) NOT NULL,
    name VARCHAR(200) NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    weight DOUBLE,
    released_on DATE,
    PRIMARY KEY (id),
    UNIQUE KEY uq_products_sku (sku)
) ENGINE=InnoDB COMMENT='商品';

CREATE TABLE orders (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    customer_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' COMMENT '注文状態',
    placed_at DATETIME(6) NOT NULL,
    shipped_at DATETIME(6),
    total DECIMAL(10,2) NOT NULL,
    PRIMARY KEY (id),
    KEY ix_orders_customer (customer_id),
    KEY ix_orders_status_placed (status, placed_at),
    CONSTRAINT ct_orders_customer FOREIGN KEY (customer_id) REFERENCES customers (id) ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB COMMENT='注文';

CREATE TABLE order_lines (
    order_id BIGINT UNSIGNED NOT NULL,
    line_no INT NOT NULL,
    product_id INT NOT NULL,
    quantity INT NOT NULL DEFAULT 1,
    unit_price DECIMAL(10,2) NOT NULL,
    PRIMARY KEY (order_id, line_no),
    KEY ix_order_lines_product (product_id),
    CONSTRAINT ct_order_lines_order FOREIGN KEY (order_id) REFERENCES orders (id) ON DELETE CASCADE,
    CONSTRAINT ct_order_lines_product FOREIGN KEY (product_id) REFERENCES products (id)
) ENGINE=InnoDB COMMENT='注文明細';

CREATE TABLE shipments (
    order_id BIGINT UNSIGNED NOT NULL,
    line_no INT NOT NULL,
    carrier VARCHAR(64) NOT NULL,
    tracking_no VARCHAR(64),
    shipped_on DATE NOT NULL,
    PRIMARY KEY (order_id, line_no),
    UNIQUE KEY uq_shipments_tracking (carrier, tracking_no),
    CONSTRAINT ct_shipments_line FOREIGN KEY (order_id, line_no)
        REFERENCES order_lines (order_id, line_no) ON UPDATE CASCADE ON DELETE CASCADE
) ENGINE=InnoDB COMMENT='配送';

-- A view and a trigger, so that the capture carries the two statement shapes
-- the importer has to step over rather than misread: a view is written twice,
-- as a placeholder and then for real, and a trigger arrives wrapped in a
-- DELIMITER pair.
CREATE DEFINER=`jjf`@`%` SQL SECURITY INVOKER VIEW active_customers AS
    SELECT id, email FROM customers WHERE active = 1;

DELIMITER ;;
CREATE DEFINER=`jjf`@`%` TRIGGER trg_orders_placed_at BEFORE INSERT ON orders
FOR EACH ROW
BEGIN
    IF NEW.placed_at IS NULL THEN
        SET NEW.placed_at = NOW(6);
    END IF;
END ;;
DELIMITER ;
