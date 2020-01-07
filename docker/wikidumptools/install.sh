#!/bin/bash

apt-get update
apt-get install --assume-yes apt-utils
apt-get -y install locales tzdata
locale-gen pl_PL.UTF-8
update-locale LANG=pl_PL.UTF-8
ln -sf /usr/share/zoneinfo/Europe/Warsaw /etc/localtime

