DROP DATABASE IF EXISTS isuumo;
CREATE DATABASE isuumo;

DROP TABLE IF EXISTS isuumo.estate;
DROP TABLE IF EXISTS isuumo.chair;

CREATE TABLE isuumo.estate
(
    id          INTEGER             NOT NULL PRIMARY KEY,
    name        VARCHAR(64)         NOT NULL,
    description VARCHAR(4096)       NOT NULL,
    thumbnail   VARCHAR(128)        NOT NULL,
    address     VARCHAR(128)        NOT NULL,
    latitude    DOUBLE PRECISION    NOT NULL,
    longitude   DOUBLE PRECISION    NOT NULL,
    rent        INTEGER             NOT NULL,
    door_height INTEGER             NOT NULL,
    door_width  INTEGER             NOT NULL,
    features    VARCHAR(64)         NOT NULL,
    popularity  INTEGER             NOT NULL,
    width_level  INTEGER NOT NULL DEFAULT -1,
    height_level INTEGER NOT NULL DEFAULT -1,
    rent_level   INTEGER NOT NULL DEFAULT -1
);

CREATE TABLE isuumo.chair
(
    id          INTEGER         NOT NULL PRIMARY KEY,
    name        VARCHAR(64)     NOT NULL,
    description VARCHAR(4096)   NOT NULL,
    thumbnail   VARCHAR(128)    NOT NULL,
    price       INTEGER         NOT NULL,
    height      INTEGER         NOT NULL,
    width       INTEGER         NOT NULL,
    depth       INTEGER         NOT NULL,
    color       VARCHAR(64)     NOT NULL,
    features    VARCHAR(64)     NOT NULL,
    kind        VARCHAR(64)     NOT NULL,
    popularity  INTEGER         NOT NULL,
    stock       INTEGER         NOT NULL,
    width_level  INTEGER NOT NULL DEFAULT -1,
    height_level INTEGER NOT NULL DEFAULT -1,
    depth_level   INTEGER NOT NULL DEFAULT -1,
    price_level   INTEGER NOT NULL DEFAULT -1
);

CREATE TABLE isuumo.chair_feature
(
    chair_id         INTEGER         NOT NULL,
    feature_id       INTEGER         NOT NULL,
    PRIMARY KEY (chair_id, feature_id)
);

CREATE TABLE isuumo.estate_feature
(
    estate_id        INTEGER         NOT NULL,
    feature_id       INTEGER         NOT NULL,
    PRIMARY KEY (estate_id, feature_id)
);

CREATE INDEX estate1 ON isuumo.estate (door_width, door_height, popularity, id);
CREATE INDEX estate2 ON isuumo.estate (rent, id);
CREATE INDEX estate3 ON isuumo.estate (rent, popularity, id);
CREATE INDEX estate4 ON isuumo.estate (latitude, longitude, popularity, id);
CREATE INDEX estate5 ON isuumo.estate (id, popularity);
CREATE INDEX estate6 ON isuumo.estate (height_level, width_level, popularity, id);

CREATE INDEX chair1 ON isuumo.chair (stock, price, id);
CREATE INDEX chair2 ON isuumo.chair (price, stock);
CREATE INDEX chair3 ON isuumo.chair (kind, stock);
CREATE INDEX chair4 ON isuumo.chair (price, stock, popularity, id);
