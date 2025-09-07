# WoT Relay

WoT Relay is a high-performance Nostr relay that saves all the notes from people you follow and their extended network (Web of Trust). Built on the [Khatru](https://khatru.nostr.technology) framework with production-ready features including health monitoring, structured logging, and anonymous access via Tor.

## Features

- **Web of Trust**: Automatically discovers and archives notes from your extended network
- **Performance Optimized**: Multi-threaded processing, batch operations, and memory monitoring
- **Production Ready**: Health checks, structured logging, metrics, and graceful shutdown
- **Anonymous Access**: Built-in Tor proxy for privacy-preserving access
- **Docker Support**: Complete containerization with production deployment
- **Auto-healing**: Automatic container health monitoring and restart
- **Configurable**: Extensive environment variable configuration

# Available Relays

Don't want to run the relay, just want to connect to some? Here are some available relays:

- [wss://wot.utxo.one](https://wot.utxo.one)
- [wss://nostrelites.org](https://nostrelites.org)
- [wss://wot.nostr.party](https://wot.nostr.party)
- [wss://wot.sovbit.host](https://wot.sovbit.host)
- [wss://wot.girino.org](https://wot.girino.org)
- [wss://relay.lnau.net](https://relay.lnau.net)
- [wss://wot.siamstr.com](https://wot.siamstr.com)
- [wss://relay.lexingtonbitcoin.org](https://relay.lexingtonbitcoin.org)
- [wss://wot.azzamo.net](https://wot.azzamo.net)
- [wss://wot.swarmstr.com](https://wot.swarmstr.com)
- [wss://zap.watch](https://zap.watch)
- [wss://satsage.xyz](https://satsage.xyz)
- [wss://wons.calva.dev](https://wons.calva.dev)
- [wss://wot.zacoos.com](https://wot.zacoos.com)
- [wss://wot.shaving.kiwi](https://wot.shaving.kiwi)
- [wss://wot.tealeaf.dev](https://wot.tealeaf.dev)
- [wss://wot.nostr.net](https://wot.nostr.net)
- [wss://relay.goodmorningbitcoin.com](https://relay.goodmorningbitcoin.com)
- [wss://wot.sudocarlos.com](wss://wot.sudocarlos.com)

## Prerequisites

- **Go**: Ensure you have Go installed on your system. You can download it from [here](https://golang.org/dl/).
- **Build Essentials**: If you're using Linux, you may need to install build essentials. You can do this by running `sudo apt install build-essential`.

## Setup Instructions

Follow these steps to get the WOT Relay running on your local machine:

### 1. Clone the repository

```bash
git clone https://github.com/bitvora/wot-relay.git
cd wot-relay
```

### 2. Copy `example.env` to `.env`

You'll need to create an `.env` file based on the example provided in the repository.

```bash
cp example.env .env
```

### 3. Set your environment variables

Open the `.env` file and set the necessary environment variables. Example variables include:

```bash
RELAY_NAME="YourRelayName"
RELAY_PUBKEY="YourPublicKey" # the owner's hexkey, not npub. Convert npub to hex here: https://nostrcheck.me/converter/
RELAY_DESCRIPTION="Your relay description"
DB_PATH="/home/ubuntu/wot-relay/db" # any path you would like the database to be saved.
INDEX_PATH="/home/ubuntu/wot-relay/templates/index.html" # path to the index.html file
STATIC_PATH="/home/ubuntu/wot-relay/templates/static" # path to the static folder
REFRESH_INTERVAL_HOURS=24 # interval in hours to refresh the web of trust
MINIMUM_FOLLOWERS=3 #how many followers before they're allowed in the WoT
ARCHIVAL_SYNC="FALSE" # set to TRUE to archive every note from every person in the WoT (not recommended)
ARCHIVE_REACTIONS="FALSE" # set to TRUE to archive every reaction from every person in the WoT (not recommended)
IGNORE_FOLLOWS_LIST="" # comma separated list of pubkeys who follow too many bots and ruin the WoT
```

### 4. Build the project

Run the following command to build the relay:

```bash
go build -ldflags "-X main.version=$(git describe --tags --always)"
```

### 5. Create a Systemd Service (optional)

To have the relay run as a service, create a systemd unit file. Make sure to limit the memory usage to less than your system's total memory to prevent the relay from crashing the system.

1. Create the file:

```bash
sudo nano /etc/systemd/system/wot-relay.service
```

2. Add the following contents:

```ini
[Unit]
Description=WOT Relay Service
After=network.target

[Service]
ExecStart=/home/ubuntu/wot-relay/wot-relay
WorkingDirectory=/home/ubuntu/wot-relay
Restart=always
MemoryLimit=2G

[Install]
WantedBy=multi-user.target
```

Replace `/path/to/` with the actual paths where you cloned the repository and stored the `.env` file.

3. Reload systemd to recognize the new service:

```bash
sudo systemctl daemon-reload
```

4. Start the service:

```bash
sudo systemctl start wot-relay
```

5. (Optional) Enable the service to start on boot:

```bash
sudo systemctl enable wot-relay
```

#### Permission Issues on Some Systems

the relay may not have permissions to read and write to the database. To fix this, you can change the permissions of the database folder:

```bash
sudo chmod -R 777 /path/to/db
```

### 6. Serving over nginx (optional)

You can serve the relay over nginx by adding the following configuration to your nginx configuration file:

```nginx
server {
    listen 80;
    server_name yourdomain.com;

    location / {
        proxy_pass http://localhost:3334;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

Replace `yourdomain.com` with your actual domain name.

After adding the configuration, restart nginx:

```bash
sudo systemctl restart nginx
```

### 7. Install Certbot (optional)

If you want to serve the relay over HTTPS, you can use Certbot to generate an SSL certificate.

```bash
sudo apt-get update
sudo apt-get install certbot python3-certbot-nginx
```

After installing Certbot, run the following command to generate an SSL certificate:

```bash
sudo certbot --nginx
```

Follow the instructions to generate the certificate.

### 8. Access the relay

Once everything is set up, the relay will be running on `localhost:3334` or your domain name if you set up nginx.

## Quick Start with Docker

### Development/Testing

```bash
# Clone the repository
git clone https://github.com/bitvora/wot-relay.git
cd wot-relay

# Copy environment template
cp example.env .env

# Edit .env with your configuration
nano .env

# Start with Docker Compose
docker compose up --build -d
```

### Production Deployment

For production deployment, use the optimized production configuration:

```bash
# Production deployment (includes Tor proxy)
docker-compose -f docker-compose.prod.yml up -d

# With Nginx reverse proxy
docker-compose -f docker-compose.prod.yml --profile with-nginx up -d
```

**Production Features:**
- Multi-stage optimized Docker build
- Auto-healing container monitoring
- Anonymous access via Tor proxy
- Health checks and structured logging
- Resource limits and restart policies

See [README-PRODUCTION.md](README-PRODUCTION.md) for detailed production setup instructions.

### 7. Hidden Service with Tor (optional)

Same as the step 6, but with the following command:

```sh
# in foreground
docker compose -f docker-compose.tor.yml up --build
# in background
docker compose -f docker-compose.tor.yml up --build -d
```

You can find the onion address here: `tor/data/relay/hostname`

### 8. Access the relay

Once everything is set up, the relay will be running on `localhost:3334`.

```bash
http://localhost:3334
```

## License

This project is licensed under the MIT License.
