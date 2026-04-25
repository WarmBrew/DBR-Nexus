#!/usr/bin/env python3
"""
DBR Nexus - SOCKS5 代理测试脚本

用法:
  认证模式:   python test_socks5.py --proxy 127.0.0.1:10000 --user admin --pass admin123
  无认证模式: python test_socks5.py --proxy 127.0.0.1:10000 --no-auth
  自动检测:   python test_socks5.py --proxy 127.0.0.1:10000

测试 SOCKS5 代理的连接、认证和数据传输功能。
"""

import argparse
import socket
import struct
import sys


TIMEOUT = 5  # 默认超时秒数


def log(msg):
    print(f"[*] {msg}", flush=True)


def ok(msg):
    print(f"[+] {msg}", flush=True)


def fail(msg):
    print(f"[-] {msg}", flush=True)


def warn(msg):
    print(f"[!] {msg}", flush=True)


def tcp_connect(host, port):
    """建立 TCP 连接，返回 socket 或 None"""
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(TIMEOUT)
    try:
        sock.connect((host, port))
        return sock
    except ConnectionRefusedError:
        fail(f"TCP 连接被拒绝: {host}:{port} (确认SOCKS5隧道是否已开启)")
        return None
    except socket.timeout:
        fail(f"TCP 连接超时: {host}:{port}")
        return None
    except Exception as e:
        fail(f"TCP 连接失败: {e}")
        return None


def test_socks5_full(proxy_host, proxy_port, username, password, target_host, target_port):
    """完整 SOCKS5 握手测试 (RFC 1928 + RFC 1929)"""
    sock = tcp_connect(proxy_host, proxy_port)
    if not sock:
        return False

    ok(f"TCP 连接成功: {proxy_host}:{proxy_port}")

    # === SOCKS5 握手 (认证协商) ===
    if username:
        # 提供用户名/密码认证方法 (0x02) 和无认证方法 (0x00)
        req = b'\x05\x02\x02\x00'  # VER=5, NMETHODS=2, METHODS=[0x02, 0x00]
    else:
        # 仅提供无认证方法
        req = b'\x05\x01\x00'  # VER=5, NMETHODS=1, METHODS=[0x00]

    try:
        sock.sendall(req)
        resp = sock.recv(2)
    except Exception as e:
        fail(f"握手请求失败: {e}")
        sock.close()
        return False

    if len(resp) < 2:
        fail(f"握手响应不完整: {resp.hex()}")
        sock.close()
        return False

    ver, method = resp[0], resp[1]
    if ver != 0x05:
        fail(f"非 SOCKS5 协议, VER={ver}")
        sock.close()
        return False

    if method == 0xFF:
        fail("服务器拒绝连接: 没有可接受的认证方法 (0xFF)")
        sock.close()
        return False
    elif method == 0x02:
        ok("服务器选择: 用户名/密码认证 (0x02)")
    elif method == 0x00:
        ok("服务器选择: 无需认证 (0x00)")
    else:
        fail(f"未知认证方法: 0x{method:02x}")
        sock.close()
        return False

    # === 用户名/密码认证 (RFC 1929) ===
    if method == 0x02:
        if not username:
            fail("服务器要求认证但未提供凭据")
            sock.close()
            return False

        user_bytes = username.encode('utf-8')
        pass_bytes = password.encode('utf-8')
        auth_req = struct.pack('!BB', 0x01, len(user_bytes)) + user_bytes + struct.pack('!B', len(pass_bytes)) + pass_bytes

        try:
            sock.sendall(auth_req)
            auth_resp = sock.recv(2)
        except Exception as e:
            fail(f"认证请求失败: {e}")
            sock.close()
            return False

        if len(auth_resp) < 2 or auth_resp[1] != 0x00:
            fail(f"认证失败")
            sock.close()
            return False

        ok(f"认证成功: 用户={username}")
    else:
        log("跳过认证 (无需认证)")

    # === CONNECT 请求 ===
    target_bytes = target_host.encode('utf-8')
    connect_req = struct.pack('!BBBBB', 0x05, 0x01, 0x00, 0x03, len(target_bytes)) + target_bytes + struct.pack('!H', target_port)

    try:
        sock.sendall(connect_req)
        connect_resp = sock.recv(256)
    except Exception as e:
        fail(f"CONNECT 请求失败: {e}")
        sock.close()
        return False

    if len(connect_resp) < 4 or connect_resp[1] != 0x00:
        reply_code = connect_resp[1] if len(connect_resp) > 1 else -1
        error_map = {
            0x01: "SOCKS服务器故障",
            0x02: "连接不被允许",
            0x03: "网络不可达",
            0x04: "主机不可达",
            0x05: "连接被拒绝",
            0x06: "TTL过期",
            0x07: "不支持的命令",
            0x08: "不支持的地址类型",
        }
        err_msg = error_map.get(reply_code, f"未知错误 (0x{reply_code:02x})")
        fail(f"CONNECT 失败: {err_msg}")
        sock.close()
        return False

    ok(f"CONNECT 成功: {target_host}:{target_port}")

    # === 通过代理发送 HTTP 请求 ===
    http_req = f"GET / HTTP/1.1\r\nHost: {target_host}\r\nConnection: close\r\n\r\n".encode('utf-8')
    try:
        sock.sendall(http_req)
    except Exception as e:
        fail(f"发送 HTTP 请求失败: {e}")
        sock.close()
        return False

    ok("HTTP 请求已发送")

    # === 读取响应 ===
    sock.settimeout(15)
    response = b""
    try:
        while True:
            chunk = sock.recv(4096)
            if not chunk:
                break
            response += chunk
    except socket.timeout:
        pass
    except Exception as e:
        log(f"读取响应时异常: {e}")

    sock.close()

    if response:
        first_line = response.split(b'\r\n')[0].decode('utf-8', errors='replace')
        ok(f"收到响应: {first_line}")
        body_start = response.find(b'\r\n\r\n')
        if body_start > 0:
            body = response[body_start + 4:body_start + 204]
            preview = body[:100].decode('utf-8', errors='replace').replace('\n', ' ')
            print(f"    响应预览: {preview}...")
        return True
    else:
        fail("未收到响应")
        return False


def test_no_auth_rejected(proxy_host, proxy_port):
    """测试: 认证模式下，不支持认证的客户端应被拒绝"""
    sock = tcp_connect(proxy_host, proxy_port)
    if not sock:
        return False

    # 只发送无认证方法
    req = b'\x05\x01\x00'
    try:
        sock.sendall(req)
        resp = sock.recv(2)
    except Exception as e:
        fail(f"请求失败: {e}")
        sock.close()
        return False

    sock.close()

    if len(resp) >= 2 and resp[1] == 0xFF:
        ok("正确: 服务器拒绝了不支持认证的客户端 (0xFF)")
        return True
    elif len(resp) >= 2 and resp[1] == 0x00:
        warn("服务器允许无认证连接 (无认证模式)")
        return True
    else:
        fail(f"预期 0xFF 或 0x00, 收到: {resp.hex()}")
        return False


def test_wrong_credentials(proxy_host, proxy_port, username):
    """测试: 错误的凭据应被拒绝 (仅认证模式)"""
    sock = tcp_connect(proxy_host, proxy_port)
    if not sock:
        return False

    req = b'\x05\x01\x02'
    try:
        sock.sendall(req)
        resp = sock.recv(2)
    except Exception as e:
        fail(f"握手失败: {e}")
        sock.close()
        return False

    if len(resp) < 2:
        fail("握手响应不完整")
        sock.close()
        return False

    if resp[1] != 0x02:
        warn("服务器不支持认证方法 (无认证模式，跳过此测试)")
        sock.close()
        return True  # 无认证模式下这个测试无意义

    # 发送错误密码
    user_bytes = username.encode('utf-8')
    pass_bytes = b"wrong_password_12345"
    auth_req = struct.pack('!BB', 0x01, len(user_bytes)) + user_bytes + struct.pack('!B', len(pass_bytes)) + pass_bytes

    try:
        sock.sendall(auth_req)
        auth_resp = sock.recv(2)
    except Exception as e:
        fail(f"认证请求失败: {e}")
        sock.close()
        return False

    sock.close()

    if auth_resp[1] != 0x00:
        ok("正确: 服务器拒绝了错误凭据")
        return True
    else:
        fail("安全漏洞: 服务器接受了错误凭据!")
        return False


def detect_auth_mode(proxy_host, proxy_port):
    """探测代理的认证模式: auth / no-auth / unreachable"""
    sock = tcp_connect(proxy_host, proxy_port)
    if not sock:
        return "unreachable"

    # 先发带认证方法的请求，看服务器选哪个
    req = b'\x05\x02\x02\x00'  # METHODS=[0x02, 0x00]
    try:
        sock.sendall(req)
        resp = sock.recv(2)
    except Exception as e:
        sock.close()
        return "error"

    sock.close()

    if len(resp) >= 2 and resp[0] == 0x05:
        if resp[1] == 0x02:
            return "auth"
        elif resp[1] == 0x00:
            return "no-auth"
        elif resp[1] == 0xFF:
            return "no-method"
    return "error"


def main():
    parser = argparse.ArgumentParser(description='DBR Nexus - SOCKS5 代理测试脚本')
    parser.add_argument('--proxy', default='127.0.0.1:10000', help='SOCKS5代理地址 (默认: 127.0.0.1:10000)')
    parser.add_argument('--user', default='', help='认证用户名')
    parser.add_argument('--pass', dest='password', default='', help='认证密码')
    parser.add_argument('--no-auth', action='store_true', help='强制无认证模式')
    parser.add_argument('--target', default='httpbin.org:80', help='测试目标地址 (默认: httpbin.org:80)')
    args = parser.parse_args()

    proxy_parts = args.proxy.split(':')
    proxy_host = proxy_parts[0]
    proxy_port = int(proxy_parts[1]) if len(proxy_parts) > 1 else 1080

    target_parts = args.target.split(':')
    target_host = target_parts[0]
    target_port = int(target_parts[1]) if len(target_parts) > 1 else 80

    username = args.user
    password = args.password

    if args.no_auth:
        username = ""
        password = ""

    print("=" * 60)
    print("  DBR Nexus - SOCKS5 代理测试")
    print("=" * 60)
    print(f"  代理地址:   {proxy_host}:{proxy_port}")
    print(f"  认证模式:   {'无认证' if not username else f'用户名/密码 ({username})'}")
    if username:
        print(f"  认证密码:   {'*' * len(password)}")
    print(f"  测试目标:   {target_host}:{target_port}")
    print("=" * 60)
    print()

    # 如果没有指定认证信息，先探测代理模式
    if not username and not args.no_auth:
        log("未指定认证信息，正在探测代理模式...")
        mode = detect_auth_mode(proxy_host, proxy_port)
        if mode == "unreachable":
            fail("无法连接到代理，请确认代理已启动")
            return 1
        elif mode == "auth":
            warn("代理要求认证，请使用 --user 和 --pass 参数指定凭据")
            print()
            print("  示例:")
            print(f"    python test_socks5.py --proxy {args.proxy} --user admin --pass yourpassword")
            return 1
        elif mode == "no-auth":
            ok("探测结果: 无认证模式")
        elif mode == "no-method":
            fail("代理拒绝了所有认证方法")
            return 1
        else:
            fail(f"探测失败: {mode}")
            return 1
        print()

    results = []

    # 测试1: 完整流程 (认证 + CONNECT + HTTP)
    log("=== 测试1: 完整 SOCKS5 流程 ===")
    r = test_socks5_full(proxy_host, proxy_port, username, password, target_host, target_port)
    results.append(("完整流程", r))
    print()

    # 测试2: 无认证客户端检测
    log("=== 测试2: 认证协商行为 ===")
    r = test_no_auth_rejected(proxy_host, proxy_port)
    results.append(("认证协商", r))
    print()

    # 测试3: 错误凭据 (仅认证模式有意义)
    log("=== 测试3: 错误凭据检测 ===")
    if username:
        r = test_wrong_credentials(proxy_host, proxy_port, username)
        results.append(("错误凭据拒绝", r))
    else:
        log("无认证模式，跳过此测试")
        results.append(("错误凭据拒绝", True))
    print()

    # 汇总
    print("=" * 60)
    print("  测试结果汇总")
    print("=" * 60)
    passed = 0
    for name, result in results:
        status = "PASS" if result else "FAIL"
        print(f"  [{status}] {name}")
        if result:
            passed += 1

    print()
    print(f"  通过: {passed}/{len(results)}")
    print("=" * 60)

    if passed == len(results):
        print("\n  所有测试通过! SOCKS5 代理工作正常。\n")
        print("  可用命令:")
        if username:
            print(f"    curl -x socks5h://{username}:{password}@{proxy_host}:{proxy_port} https://httpbin.org/ip")
        else:
            print(f"    curl -x socks5h://{proxy_host}:{proxy_port} https://httpbin.org/ip")
        return 0
    else:
        print("\n  部分测试失败，请检查代理配置。\n")
        return 1


if __name__ == '__main__':
    sys.exit(main())
