#!/bin/bash

cd ~

go get main
go build main

rm -f ~/install3.sh
echo 'export PS1="\e[0;32m[\u@\h \w]\$ \e[m"' >> ~/.bashrc
chmod +x ~/.bashrc

cd ~

