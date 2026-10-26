#!/bin/sh
set -eu

if [ -e .env ]; then
    printf '%s\n' '.env already exists'
    exit 0
fi

umask 077
mysql_password=$(openssl rand -hex 32)
mysql_root_password=$(openssl rand -hex 32)
jwt_secret=$(openssl rand -hex 32)
sed \
    -e "s/^MYSQL_PASSWORD=.*/MYSQL_PASSWORD=$mysql_password/" \
    -e "s/^MYSQL_ROOT_PASSWORD=.*/MYSQL_ROOT_PASSWORD=$mysql_root_password/" \
    -e "s/^JWT_SECRET=.*/JWT_SECRET=$jwt_secret/" \
    .env.example > .env
printf '%s\n' '.env created with random local credentials'
