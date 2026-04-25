/**
 * Browser-side encryption module using Web Crypto API.
 * Implements AES-256-GCM encryption with P-256 ECDH key exchange,
 * matching the Go server's encryption protocol.
 *
 * IMPORTANT: Web Crypto API (crypto.subtle) is only available in secure contexts:
 * - HTTPS connections
 * - localhost (http://localhost or http://127.0.0.1)
 * - file:// URLs
 *
 * Accessing via http://192.168.x.x or other non-localhost HTTP addresses will fail.
 */

// HKDF info labels must match the Go side
const BROWSER_SESSION_LABEL = 'dbr-nexus-browser-v1';

/**
 * Check if Web Crypto API is available (secure context required).
 * Returns an error message if not available, or null if available.
 */
export function checkSecureContext(): string | null {
  if (typeof crypto === 'undefined') {
    return 'Web Crypto API not available in this environment';
  }
  if (!crypto.subtle) {
    const isSecure = window.location.protocol === 'https:' ||
                     window.location.hostname === 'localhost' ||
                     window.location.hostname === '127.0.0.1';
    if (!isSecure) {
      return `Web Crypto API requires a secure context (HTTPS or localhost).
Current URL: ${window.location.href}
Please access the server via:
- HTTPS (recommended)
- http://localhost:${window.location.port}
- http://127.0.0.1:${window.location.port}`;
    }
    return 'Web Crypto API (crypto.subtle) is not available';
  }
  return null;
}

// Frame constants (must match internal/crypto/frame.go)
const FRAME_VERSION = 0x01;
const FLAG_HAS_PADDING = 0x01;
const FLAG_IS_DUMMY = 0x02;
const ROUTING_HEADER_SIZE = 41;

// Padding bucket sizes
const PADDING_BUCKETS = [128, 256, 512, 1024, 2048, 4096, 8192, 16384];

// AES-GCM parameters
const GCM_NONCE_LENGTH = 12;
const GCM_TAG_LENGTH = 128; // bits

// Max frame size (1MB) — matches Go side MaxFrameSize
const MAX_FRAME_SIZE = 1 << 20;

export class CryptoSession {
  private encryptKey!: CryptoKey;
  private hmacKey!: CryptoKey;       // cached HMAC key
  private nonceBase!: ArrayBuffer;
  private seqNum: number = 0;
  private lastRecvSeq: number = 0;
  private ready: boolean = false;

  get isReady(): boolean {
    return this.ready;
  }

  /**
   * Perform the browser-side key exchange with the server.
   * Called after WebSocket connection is established.
   * Throws an error if Web Crypto API is not available (non-secure context).
   */
  static async handshake(
    ws: WebSocket,
    jwtToken: string,
    serverHello: { server_pub_key: string; server_nonce: string }
  ): Promise<CryptoSession> {
    // Check secure context before attempting key generation
    const secureContextError = checkSecureContext();
    if (secureContextError) {
      throw new Error(secureContextError);
    }

    const session = new CryptoSession();

    // Decode server's P-256 public key (uncompressed format: 04 || x || y, 65 bytes)
    const serverPubKeyBytes = base64ToArrayBuffer(serverHello.server_pub_key);
    const serverNonce = base64ToArrayBuffer(serverHello.server_nonce);

    // Generate browser's P-256 key pair
    const browserKeyPair = await crypto.subtle.generateKey(
      { name: 'ECDH', namedCurve: 'P-256' },
      false,
      ['deriveBits']
    );

    // Generate browser nonce
    const browserNonce = crypto.getRandomValues(new Uint8Array(16));

    // Export browser public key for sending to server
    const browserPubKeyRaw = await crypto.subtle.exportKey('raw', browserKeyPair.publicKey);

    // Send key exchange message
    const keyExchangeMsg = JSON.stringify({
      type: 'encryption_key_exchange',
      browser_pub_key: arrayBufferToBase64(browserPubKeyRaw),
      browser_nonce: arrayBufferToBase64(browserNonce),
    });
    ws.send(keyExchangeMsg);

    // Import server public key
    const serverPubKey = await crypto.subtle.importKey(
      'raw',
      serverPubKeyBytes,
      { name: 'ECDH', namedCurve: 'P-256' },
      false,
      []
    );

    // Compute ECDH shared secret using the private key (CryptoKey), not raw bits
    const sharedSecret = await crypto.subtle.deriveBits(
      { name: 'ECDH', public: serverPubKey },
      browserKeyPair.privateKey,
      256
    );

    // Derive session keys via HKDF
    // Auth tag = SHA-256(JWT token)
    const jwtHash = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(jwtToken));

    // HKDF info: label || serverNonce || browserNonce || jwtHash
    const infoData = new Uint8Array(
      BROWSER_SESSION_LABEL.length + 16 + 16 + 32
    );
    const encoder = new TextEncoder();
    infoData.set(encoder.encode(BROWSER_SESSION_LABEL), 0);
    infoData.set(new Uint8Array(serverNonce), BROWSER_SESSION_LABEL.length);
    infoData.set(new Uint8Array(browserNonce), BROWSER_SESSION_LABEL.length + 16);
    infoData.set(new Uint8Array(jwtHash), BROWSER_SESSION_LABEL.length + 32);

    // Import shared secret as a raw key for HKDF deriveBits
    const sharedKey = await crypto.subtle.importKey(
      'raw',
      sharedSecret,
      { name: 'HKDF' },
      false,
      ['deriveBits']
    );

    // Derive 92 bytes: 32 (encrypt) + 32 (mac) + 12 (nonce) + 16 (code map seed)
    const derivedBits = await crypto.subtle.deriveBits(
      {
        name: 'HKDF',
        hash: 'SHA-256',
        salt: new Uint8Array(0),
        info: infoData,
      },
      sharedKey,
      92 * 8 // bits
    );

    const derived = new Uint8Array(derivedBits);

    // Extract keys
    const encryptKeyBytes = derived.slice(0, 32);
    const macKeyBytes = derived.slice(32, 64);
    // Use ArrayBuffer.slice() to create an independent copy of nonceBase
    // (not a view) — this avoids issues with detached buffers after crypto operations
    session.nonceBase = derived.buffer.slice(64, 76);

    // Import AES-GCM key
    session.encryptKey = await crypto.subtle.importKey(
      'raw',
      encryptKeyBytes,
      { name: 'AES-GCM' },
      false,
      ['encrypt', 'decrypt']
    );

    // Import and cache HMAC key (avoid re-import on every frame)
    session.hmacKey = await crypto.subtle.importKey(
      'raw',
      macKeyBytes,
      { name: 'HMAC', hash: 'SHA-256' },
      false,
      ['sign']
    );

    session.ready = true;
    return session;
  }

  /**
   * Encrypt an Envelope into a binary frame.
   */
  async encryptFrame(env: any, deviceAlias: number = 0, refId: string = ''): Promise<ArrayBuffer> {
    const seqNum = this.nextSeqNum();
    const timestamp = Date.now();

    // Serialize inner envelope as JSON
    const innerJSON = new TextEncoder().encode(JSON.stringify(env));

    // Build routing header
    const header = new ArrayBuffer(ROUTING_HEADER_SIZE);
    const headerView = new DataView(header);
    const headerBytes = new Uint8Array(header);

    // Message ID (16 bytes from UUID string)
    const msgIdBytes = uuidToBytes(env.id);
    headerBytes.set(msgIdBytes, 0);

    // Reference ID
    const refIdBytes = refId ? uuidToBytes(refId) : new Uint8Array(16);
    headerBytes.set(refIdBytes, 16);

    // Channel code, action code, type code
    const channelCode = simpleHash(env.channel || '');
    const actionCode = simpleHash(env.action || '');
    headerView.setUint16(32, channelCode, false);
    headerView.setUint16(34, actionCode, false);
    headerBytes[36] = typeToCode(env.type);
    headerView.setUint32(37, deviceAlias, false);

    // Compute header HMAC
    const headerMAC = await this.computeHeaderMAC(headerBytes);

    // Skip padding for stream messages (shell, tunnel) to reduce latency
    const estimatedEncPayloadLen = GCM_NONCE_LENGTH + innerJSON.length + 16;
    const frameBodySize = 15 + ROUTING_HEADER_SIZE + 16 + estimatedEncPayloadLen;
    let padding: Uint8Array<ArrayBuffer> = new Uint8Array(0);
    if (env.type !== 'stream') {
      padding = computePadding(frameBodySize);
    }

    // Determine correct flags (must match what goes into the final frame)
    const flags = padding.length > 0 ? FLAG_HAS_PADDING : 0;

    // Build AAD with correct flags
    // AAD structure: version(1) + flags(1) + seqNum(4) + timestamp(8) + header(41) = 55 bytes
    // Header starts at offset 14 (not 15!)
    const aad = new Uint8Array(1 + 1 + 4 + 8 + ROUTING_HEADER_SIZE);
    aad[0] = FRAME_VERSION;
    aad[1] = flags;
    const aadView = new DataView(aad.buffer);
    aadView.setUint32(2, seqNum, false);
    aadView.setBigUint64(6, BigInt(timestamp), false);
    aad.set(headerBytes, 14); // Correct offset: 1+1+4+8 = 14

    // Build nonce: nonceBase[0:4] + seqNum(4) + random(4)
    // Safety check: ensure nonceBase is valid
    if (!this.nonceBase || this.nonceBase.byteLength < 4) {
      throw new Error('nonceBase not initialized or too short');
    }
    const nonce = new Uint8Array(GCM_NONCE_LENGTH);
    nonce.set(new Uint8Array(this.nonceBase).subarray(0, 4), 0);
    const nonceView = new DataView(nonce.buffer);
    nonceView.setUint32(4, seqNum, false);
    crypto.getRandomValues(nonce.subarray(8, 12));

    // Encrypt
    const ciphertext = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: nonce, additionalData: aad, tagLength: GCM_TAG_LENGTH },
      this.encryptKey,
      innerJSON
    );

    // Combine: nonce + ciphertext (includes tag)
    const encryptedPayload = new Uint8Array(GCM_NONCE_LENGTH + ciphertext.byteLength);
    encryptedPayload.set(nonce, 0);
    encryptedPayload.set(new Uint8Array(ciphertext), GCM_NONCE_LENGTH);

    // Build final frame
    const totalLen = frameBodySize + padding.length + (padding.length > 0 ? 2 : 0);
    const frame = new Uint8Array(totalLen);
    const frameView = new DataView(frame.buffer);

    frame[0] = FRAME_VERSION;
    frame[1] = flags;
    frameView.setUint32(2, seqNum, false);
    frameView.setBigUint64(6, BigInt(timestamp), false);
    frame[14] = ROUTING_HEADER_SIZE;

    let offset = 15;
    frame.set(headerBytes, offset);
    offset += ROUTING_HEADER_SIZE;
    frame.set(new Uint8Array(headerMAC), offset);
    offset += 16;
    frame.set(encryptedPayload, offset);
    offset += encryptedPayload.length;

    if (padding.length > 0) {
      frame.set(padding, offset);
      offset += padding.length;
      frameView.setUint16(offset, padding.length, false);
    }

    return frame.buffer;
  }

  /**
   * Decrypt a binary frame into an Envelope.
   * Returns { env, isDummy } or throws on error.
   */
  async decryptFrame(frameData: ArrayBuffer): Promise<{ env: any | null; isDummy: boolean }> {
    const data = new Uint8Array(frameData);
    if (data.length < 15) throw new Error('frame too short');
    if (data.length > MAX_FRAME_SIZE) throw new Error(`frame too large: ${data.length}`);

    const version = data[0];
    if (version !== FRAME_VERSION) throw new Error(`unsupported version: ${version}`);

    const flags = data[1];
    // Use DataView on the underlying buffer with correct byteOffset
    // (data may be a view with non-zero byteOffset)
    const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
    const seqNum = view.getUint32(2, false);
    const timestamp = Number(view.getBigUint64(6, false));

    const isDummy = (flags & FLAG_IS_DUMMY) !== 0;

    // Anti-replay check
    if (seqNum <= this.lastRecvSeq && this.lastRecvSeq !== 0) {
      throw new Error(`replayed seq: ${seqNum} <= ${this.lastRecvSeq}`);
    }
    this.lastRecvSeq = seqNum;

    // Timestamp skew check (900s = 15min)
    const now = Date.now();
    if (Math.abs(now - timestamp) > 900000) {
      throw new Error(`timestamp skew: ${Math.abs(now - timestamp)}ms`);
    }

    if (isDummy) return { env: null, isDummy: true };

    const headerLen = data[14];
    if (headerLen !== ROUTING_HEADER_SIZE) throw new Error(`bad header len: ${headerLen}`);

    const headerBytes = data.slice(15, 15 + ROUTING_HEADER_SIZE);
    const headerMAC = data.slice(15 + ROUTING_HEADER_SIZE, 15 + ROUTING_HEADER_SIZE + 16);

    // Verify header HMAC
    const expectedMAC = await this.computeHeaderMAC(headerBytes);
    if (!constantTimeEqual(headerMAC, new Uint8Array(expectedMAC))) {
      throw new Error('header HMAC mismatch');
    }

    // Build AAD
    // AAD structure: version(1) + flags(1) + seqNum(4) + timestamp(8) + header(41) = 55 bytes
    // Header starts at offset 14 (not 15!)
    const aad = new Uint8Array(1 + 1 + 4 + 8 + ROUTING_HEADER_SIZE);
    aad[0] = version;
    aad[1] = flags;
    const aadView = new DataView(aad.buffer);
    aadView.setUint32(2, seqNum, false);
    aadView.setBigUint64(6, BigInt(timestamp), false);
    aad.set(headerBytes, 14); // Correct offset: 1+1+4+8 = 14

    // Extract encrypted payload
    let payloadStart = 15 + ROUTING_HEADER_SIZE + 16;
    let payloadEnd = data.length;

    // Check for padding
    if (flags & FLAG_HAS_PADDING) {
      // Use view (with correct byteOffset) to read padding length
      const paddingLen = view.getUint16(data.length - 2, false);
      payloadEnd = data.length - paddingLen - 2;
    }

    const encryptedPayload = data.slice(payloadStart, payloadEnd);

    if (encryptedPayload.length < GCM_NONCE_LENGTH + 16) {
      throw new Error('encrypted payload too short');
    }

    const nonce = encryptedPayload.slice(0, GCM_NONCE_LENGTH);
    const ciphertext = encryptedPayload.slice(GCM_NONCE_LENGTH);

    // Decrypt
    const plaintext = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: nonce, additionalData: aad, tagLength: GCM_TAG_LENGTH },
      this.encryptKey,
      ciphertext
    );

    // Parse envelope
    const env = JSON.parse(new TextDecoder().decode(plaintext));
    return { env, isDummy: false };
  }

  private nextSeqNum(): number {
    this.seqNum = (this.seqNum + 1) & 0xFFFFFFFF; // wrap at uint32 max
    return this.seqNum;
  }

  private async computeHeaderMAC(headerBytes: Uint8Array): Promise<ArrayBuffer> {
    // Web Crypto API sign() accepts ArrayBufferView (Uint8Array), which handles byteOffset correctly
    const full = await crypto.subtle.sign('HMAC', this.hmacKey, headerBytes);
    // Truncate to 128 bits and ensure it's ArrayBuffer (not ArrayBufferLike)
    const truncated = new Uint8Array(full).slice(0, 16);
    // slice() creates a view, but we need the underlying buffer
    // Use slice() on the ArrayBuffer to get an independent copy
    return truncated.buffer.slice(truncated.byteOffset, truncated.byteOffset + truncated.byteLength);
  }

  /**
   * Create a dummy frame for traffic obfuscation.
   */
  async encryptDummy(): Promise<ArrayBuffer> {
    const seqNum = this.nextSeqNum();
    const timestamp = Date.now();

    const dummyData = crypto.getRandomValues(new Uint8Array(64));
    const msgId = crypto.getRandomValues(new Uint8Array(16));

    const header = new ArrayBuffer(ROUTING_HEADER_SIZE);
    const headerBytes = new Uint8Array(header);
    headerBytes.set(msgId, 0);

    const headerMAC = await this.computeHeaderMAC(headerBytes);

    // Estimate encrypted payload size
    const estimatedEncPayloadLen = GCM_NONCE_LENGTH + dummyData.length + 16;
    const frameBodySize = 15 + ROUTING_HEADER_SIZE + 16 + estimatedEncPayloadLen;
    const padding = computePadding(frameBodySize);

    // Determine flags BEFORE AAD
    const flags = FLAG_IS_DUMMY | (padding.length > 0 ? FLAG_HAS_PADDING : 0);

    const aad = new Uint8Array(1 + 1 + 4 + 8 + ROUTING_HEADER_SIZE);
    aad[0] = FRAME_VERSION;
    aad[1] = flags;
    new DataView(aad.buffer).setUint32(2, seqNum, false);
    new DataView(aad.buffer).setBigUint64(6, BigInt(timestamp), false);
    aad.set(headerBytes, 14); // Correct offset: 1+1+4+8 = 14

    // Safety check: ensure nonceBase is valid
    if (!this.nonceBase || this.nonceBase.byteLength < 4) {
      throw new Error('nonceBase not initialized or too short');
    }
    const nonce = new Uint8Array(GCM_NONCE_LENGTH);
    nonce.set(new Uint8Array(this.nonceBase).subarray(0, 4), 0);
    new DataView(nonce.buffer).setUint32(4, seqNum, false);
    crypto.getRandomValues(nonce.subarray(8, 12));

    const ciphertext = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: nonce, additionalData: aad, tagLength: GCM_TAG_LENGTH },
      this.encryptKey,
      dummyData
    );

    const encryptedPayload = new Uint8Array(GCM_NONCE_LENGTH + ciphertext.byteLength);
    encryptedPayload.set(nonce, 0);
    encryptedPayload.set(new Uint8Array(ciphertext), GCM_NONCE_LENGTH);

    const totalLen = frameBodySize + padding.length + (padding.length > 0 ? 2 : 0);
    const frame = new Uint8Array(totalLen);
    const frameView = new DataView(frame.buffer);

    frame[0] = FRAME_VERSION;
    frame[1] = flags;
    frameView.setUint32(2, seqNum, false);
    frameView.setBigUint64(6, BigInt(timestamp), false);
    frame[14] = ROUTING_HEADER_SIZE;

    let offset = 15;
    frame.set(headerBytes, offset); offset += ROUTING_HEADER_SIZE;
    frame.set(new Uint8Array(headerMAC), offset); offset += 16;
    frame.set(encryptedPayload, offset); offset += encryptedPayload.length;
    if (padding.length > 0) {
      frame.set(padding, offset); offset += padding.length;
      frameView.setUint16(offset, padding.length, false);
    }

    return frame.buffer;
  }
}

// --- Utility functions ---

function base64ToArrayBuffer(b64: string): ArrayBuffer {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

function arrayBufferToBase64(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

function uuidToBytes(id: string): Uint8Array {
  const result = new Uint8Array(16);
  if (id.length !== 36) {
    // Fallback: hash
    for (let i = 0; i < 16 && i < id.length; i++) result[i] = id.charCodeAt(i);
    return result;
  }
  const hex = id.slice(0, 8) + id.slice(9, 13) + id.slice(14, 18) + id.slice(19, 23) + id.slice(24, 36);
  for (let i = 0; i < 16; i++) {
    result[i] = parseInt(hex.substr(i * 2, 2), 16);
  }
  return result;
}

function simpleHash(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = ((h << 5) - h + s.charCodeAt(i)) | 0;
  }
  return Math.abs(h) & 0xFFFF;
}

function typeToCode(t: string): number {
  switch (t) {
    case 'request': return 0x01;
    case 'response': return 0x02;
    case 'stream': return 0x03;
    case 'event': return 0x04;
    default: return 0x00;
  }
}

function computePadding(currentSize: number): Uint8Array<ArrayBuffer> {
  const maxSize = PADDING_BUCKETS[PADDING_BUCKETS.length - 1];
  if (currentSize >= maxSize) return new Uint8Array(0);

  let targetSize = maxSize;
  for (const bucket of PADDING_BUCKETS) {
    if (bucket >= currentSize + 2) {
      targetSize = bucket;
      break;
    }
  }

  const paddingLen = targetSize - currentSize - 2;
  if (paddingLen <= 0) return new Uint8Array(0);

  const arr = new Uint8Array(paddingLen);
  crypto.getRandomValues(arr);
  return arr;
}

function constantTimeEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let result = 0;
  for (let i = 0; i < a.length; i++) {
    result |= a[i] ^ b[i];
  }
  return result === 0;
}
