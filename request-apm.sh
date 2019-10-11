#!/bin/bash

while true
do
	if (( $RANDOM % 2 )); then
		if (( $RANDOM % 2 )); then
		    echo "Request to root \"/\" route as edu"
		    curl localhost:8080?name=edu
	        else
		    echo "Request to root \"/\" route as guest"
		    curl localhost:8080/
		fi

	else
	        echo "Request to unknown route as guest"
	        curl localhost:8080/fuckroute
	fi

	sleep 0.5
done
