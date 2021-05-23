
# Build the docker image
docker-build:
		docker build . -t docker.io/cnvrg/config-reloader:latest

# Push the docker image
docker-push:
	docker push docker.io/cnvrg/config-reloader:latest