#!/bin/bash -e
docker build -t tcpproxy .
docker tag tcpproxy $DOCKER_USERNAME/tcpproxy:${TRAVIS_TAG:1}
docker tag tcpproxy $DOCKER_USERNAME/tcpproxy:latest
echo "$DOCKER_PASSWORD" | docker login -u "$DOCKER_USERNAME" --password-stdin
docker push $DOCKER_USERNAME/tcpproxy:${TRAVIS_TAG:1}
docker push $DOCKER_USERNAME/tcpproxy:latest
