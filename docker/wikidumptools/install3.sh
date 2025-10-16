#!/bin/bash

cd ~

rm go.mod go.sum
go mod init wikidumptools
go mod tidy
go build -o run ./main

rm -f ~/install3.sh
echo 'export PS1="\e[0;32m[\u@\h \w]\$ \e[m"' >> ~/.bashrc
chmod +x ~/.bashrc

cd ~

