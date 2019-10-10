#!/bin/bash

while true
do
	timestamp=`date +"%s"`
	if (( $RANDOM % 2 )); then
		echo "Request to root \"/\" route"
		curl localhost:8080?name=edu
	else
		echo "Request to unknown route"
		curl localhost:8080/fuckroute?name=rafa
	fi

	sleep 0.3
done
