#!/bin/bash

: "${DATA_DIR?Wykonaj jakis ../env/env-*.sh}"

mkdir -p ${DATA_DIR}/cache/pageview/{months,days,hours}
mkdir -p ${DATA_DIR}/cache/dumps/{wikipedia,wikiquote}
mkdir -p ${DATA_DIR}/cache/clickstream
mkdir -p ${DATA_DIR}/public/{wikipedia,wikiquote}
mkdir -p ${DATA_DIR}/logs
mkdir -p ${DATA_DIR}/db

