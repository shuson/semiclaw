import socket
import sys
import time

def tcp_ping(host, port, timeout=3):
    try:
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(timeout)
        start_time = time.time()
        result = sock.connect_ex((host, port))
        end_time = time.time()
        
        if result == 0:
            print(f"Port {port} is open on {host}")
            print(f"Response time: {(end_time - start_time)*1000:.2f} ms")
            return True
        else:
            print(f"Port {port} is closed on {host}")
            return False
    except Exception as e:
        print(f"Error: {e}")
        return False
    finally:
        sock.close()

if __name__ == "__main__":
    host = "38.100.152.134"
    port = 30095
    tcp_ping(host, port)