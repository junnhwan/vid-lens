// 增量 MD5(RFC 1321),用于分片上传的 file_md5 标识与断点续传。
// 后端把 file_md5 作为分片会话键与资产去重键,所以必须是真实 MD5;
// 浏览器 WebCrypto 不支持 MD5,这里自带一份纯 TS 实现(无新依赖)。

const SHIFT = [
  7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22,
  5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20,
  4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23,
  6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21,
]

// K[i] = floor(2^32 * |sin(i+1)|),运行期生成避免 64 个常量占位
const K = new Int32Array(64)
for (let i = 0; i < 64; i++) K[i] = Math.floor(Math.abs(Math.sin(i + 1)) * 4294967296)

export class MD5 {
  private state = new Int32Array([0x67452301, -271733879, -1732584194, 0x10325476])
  private buffer = new Uint8Array(64)
  private bufferLen = 0
  private byteLen = 0

  update(data: Uint8Array | ArrayBuffer): this {
    const bytes = data instanceof Uint8Array ? data : new Uint8Array(data)
    this.byteLen += bytes.length
    let offset = 0
    if (this.bufferLen > 0) {
      const take = Math.min(64 - this.bufferLen, bytes.length)
      this.buffer.set(bytes.subarray(0, take), this.bufferLen)
      this.bufferLen += take
      offset = take
      if (this.bufferLen === 64) {
        this.processBlock(this.buffer, 0)
        this.bufferLen = 0
      }
    }
    while (offset + 64 <= bytes.length) {
      this.processBlock(bytes, offset)
      offset += 64
    }
    if (offset < bytes.length) {
      this.buffer.set(bytes.subarray(offset), 0)
      this.bufferLen = bytes.length - offset
    }
    return this
  }

  digestHex(): string {
    // padding: 0x80 + zeros,末尾 8 字节小端 bit 长度
    const bitLen = this.byteLen * 8 // < 2^53,精确
    const lo = bitLen % 4294967296
    const hi = Math.floor(bitLen / 4294967296)
    const padLen = this.bufferLen < 56 ? 56 - this.bufferLen : 120 - this.bufferLen
    const tail = new Uint8Array(padLen + 8)
    tail[0] = 0x80
    new DataView(tail.buffer).setUint32(padLen, lo, true)
    new DataView(tail.buffer).setUint32(padLen + 4, hi, true)
    // 保存状态,digest 后恢复,使实例在 digest 后仍可继续 update(便于测试)
    const saved = Int32Array.from(this.state)
    const savedLen = this.bufferLen
    const savedBuf = Uint8Array.from(this.buffer.subarray(0, this.bufferLen))
    this.update(tail)
    const hex = Array.from(this.state)
      .map(word => {
        // 状态字按小端输出
        const bytes = new Uint8Array(4)
        new DataView(bytes.buffer).setUint32(0, word >>> 0, true)
        return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('')
      })
      .join('')
    this.state = saved
    this.buffer = new Uint8Array(64)
    this.buffer.set(savedBuf)
    this.bufferLen = savedLen
    this.byteLen -= tail.length
    return hex
  }

  // 每块 64 字节的 MD5 变换
  private processBlock(bytes: Uint8Array, offset: number) {
    // little-endian 读取 16 个 32 位字
    const m = new Int32Array(16)
    for (let i = 0; i < 16; i++) {
      const j = offset + i * 4
      m[i] = bytes[j] | (bytes[j + 1] << 8) | (bytes[j + 2] << 16) | (bytes[j + 3] << 24)
    }
    let [a, b, c, d] = [this.state[0], this.state[1], this.state[2], this.state[3]]
    for (let i = 0; i < 64; i++) {
      let f: number
      let g: number
      if (i < 16) { f = (b & c) | (~b & d); g = i }
      else if (i < 32) { f = (d & b) | (~d & c); g = (5 * i + 1) % 16 }
      else if (i < 48) { f = b ^ c ^ d; g = (3 * i + 5) % 16 }
      else { f = c ^ (b | ~d); g = (7 * i) % 16 }
      const tmp = d
      d = c
      c = b
      const sum = (a + f + K[i] + m[g]) | 0
      b = (b + ((sum << SHIFT[i]) | (sum >>> (32 - SHIFT[i])))) | 0
      a = tmp
    }
    this.state[0] = (this.state[0] + a) | 0
    this.state[1] = (this.state[1] + b) | 0
    this.state[2] = (this.state[2] + c) | 0
    this.state[3] = (this.state[3] + d) | 0
  }
}

export function md5Hex(data: Uint8Array | ArrayBuffer): string {
  return new MD5().update(data).digestHex()
}
