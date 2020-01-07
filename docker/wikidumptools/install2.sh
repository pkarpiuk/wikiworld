#!/bin/bash

apt-get update
apt-get install -y vim mc sudo wget ruby curl golang git build-essential patch ruby-dev zlib1g-dev liblzma-dev software-properties-common
gem install nokogiri

useradd -ms /bin/bash ubuntu
passwd -d ubuntu
usermod -a -G sudo ubuntu

mkdir -p /db
chown -R ubuntu.ubuntu /db

