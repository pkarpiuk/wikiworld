#!/bin/bash

display_usage() { 
  echo "Commands:"
  echo "  daily"
  echo "  hourly"
  echo "  switch"
} 

# if less than one argument supplied, display usage 
if [  $# -lt 1 ] 
then 
  display_usage
  exit 1
fi 
 
# check whether user had supplied -h or --help . If yes display usage 
if [[ ( $1 == "--help") ||  $1 == "-h" ]] 
then 
  display_usage
  exit 0
fi 

export DATA_DIR=/db

case $1 in
  "daily")
    exec ./worker.rb daily
    ;;
  "hourly")
    exec ./worker.rb hourly
    ;;
  "switch")
    shift
    # ./tester.rb "$@"
    ;;
  *)
    display_usage
    exit 1
    ;;
esac

