# Welcome to StackEdit!

This is an example APM stack for auditing a Golang application.

## To run
To run this example, follow these steps:

 - Run docker-compose stack:
`sudo docker-compose up`
 - Build application
`Makebuild`
 - Run application
`./bin/golang-service-apm`
 -   Make requests to generate data on APM server
`watch -n 0,5 "curl localhost:8080?name=edu; curl localhost:8080/fuckroute?name=edu"`
