# ZenTalk API

> HTTP/WebSocket API server for ZenTalk client applications

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Overview

ZenTalk API provides a REST API and WebSocket server for client applications (web, mobile) to connect to the decentralized ZenTalk messaging network. This server handles user sessions, message encryption/decryption, and communication with the ZenTalk relay network.

## Features

- **HTTP REST API** - Send messages, manage contacts, upload media
- **WebSocket Support** - Real-time message delivery and typing indicators
- **End-to-End Encryption** - Messages encrypted with Double Ratchet algorithm
- **User Sessions** - Secure session management for client apps
- **Message Persistence** - SQLite database for message history
- **Media Handling** - Encrypted media upload/download via mesh storage
- **Read Receipts** - Track message delivery and read status
- **Contact Discovery** - Find users via DHT network

## Installation

### Prerequisites

- Go 1.21 or higher
- SQLite3

### Build

```bash
go build -o api-server cmd/api-server/main.go
```

## Usage

### Start API Server

```bash
./api-server --port 3001 --relay localhost:9001 --mesh http://localhost:8081
```

### API Endpoints

#### Authentication
- `POST /api/initialize` - Initialize user session

#### Messaging
- `POST /api/send` - Send message
- `GET /api/messages/{chatId}` - Get messages
- `POST /api/delete-message` - Delete message
- `POST /api/delete-chat` - Delete chat
- `POST /api/edit-message` - Edit message
- `POST /api/mark-as-read` - Mark as read

#### Contacts
- `POST /api/discover` - Discover contact by username/address
- `POST /api/peer-info` - Get peer information
- `POST /api/block-contact` - Block contact
- `POST /api/unblock-contact` - Unblock contact

#### Media
- `POST /api/upload-media` - Upload encrypted media
- `GET /api/media/{mediaId}` - Download media

#### User Profile
- `POST /api/update-profile` - Update profile
- `POST /api/update-status` - Update status message
- `POST /api/update-username` - Change username

### WebSocket

Connect to `ws://localhost:3001/ws` with session token for real-time updates.

**Events:**
- `message` - New message received
- `typing` - Typing indicator
- `read_receipt` - Message read confirmation
- `message_deleted` - Message deletion notification
- `message_edited` - Message edit notification
- `reaction_added` - Reaction added to message

## Configuration

### Environment Variables

- `PORT` - API server port (default: 3001)
- `RELAY_ADDRESS` - Relay server address
- `MESH_API_URL` - Mesh storage API URL
- `DB_PATH` - SQLite database path

## Development

```bash
# Run tests
go test ./...

# Run with hot reload
go run cmd/api-server/main.go --port 3001
```

## Client Integration

### Initialize Session

```javascript
const response = await fetch('http://localhost:3001/api/initialize', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    wallet_address: '0x1234...',
    username: 'alice'
  })
});

const { session_token } = await response.json();
```

### Send Message

```javascript
await fetch('http://localhost:3001/api/send', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${session_token}`
  },
  body: JSON.stringify({
    to: '0x5678...',
    content: 'Hello!',
    type: 'text'
  })
});
```

### WebSocket Connection

```javascript
const ws = new WebSocket('ws://localhost:3001/ws');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);

  if (data.type === 'message') {
    console.log('New message:', data.payload);
  }
};
```

## License

MIT License - see LICENSE file for details

## Links

- [ZenTalk Node](https://github.com/ZentaChain/zentalk-node) - Run a relay/mesh node
- [ZenTalk Protocol](https://github.com/ZentaChain/zentalk-protocol) - Protocol specification
- [Website](https://zentachain.io)

## Support

- [Discord](https://discord.gg/zentachain)
- [Telegram](https://t.me/ZentaChain)
- [Email](mailto:info@zentachain.io)
