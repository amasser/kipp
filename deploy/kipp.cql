create keyspace if not exists kipp with replication = {
    'class': 'SimpleStrategy',
    'replication_factor': 1
};

use kipp;

create table if not exists files (
    id text,
    name text,
    size bigint,
    checksum text,
    timestamp timestamp,
    primary key (checksum, id)
);