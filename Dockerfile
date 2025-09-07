# Use Golang image based on Debian Bookworm
FROM golang:bookworm

# Install patch utility
RUN apt-get update && apt-get install -y patch && rm -rf /var/lib/apt/lists/*

# Set the working directory within the container
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy patch files
COPY patches/ /app/patches/

# Apply eventstore patch for QueryKindsLimit
RUN cd $(go env GOPATH)/pkg/mod/github.com/fiatjaf/eventstore@* && \
    patch -p1 < /app/patches/eventstore-querykindslimit.patch

# Copy the rest of the application source code
COPY . .

# Set fixed environment variables
ENV DB_PATH="db"
ENV INDEX_PATH="templates/index.html"
ENV STATIC_PATH="templates/static"

# touch a .env (https://github.com/bitvora/wot-relay/pull/4)
RUN touch .env

# Build the Go application
RUN go build -o wot-relay .

# Expose the port that the application will run on
EXPOSE 3334

# Set the command to run the executable
CMD ["./wot-relay"]
