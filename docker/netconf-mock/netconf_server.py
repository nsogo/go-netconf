import json
import logging
import random
import socket
import threading
from binascii import hexlify

import gevent
from gevent.monkey import patch_all
patch_all()  # noqa: E702
from gevent import sleep
import gevent.pool
import paramiko
try:
    from paramiko.py3compat import u
except ImportError:
    u = lambda x: x
from flask import Flask, jsonify, request
from lxml import etree

logger = logging.getLogger("nc_mock")
logging.basicConfig(
    level="DEBUG", format="%(asctime)s [%(levelname)s] %(message)s")

DELIM = ']]>]]>'
SESSION_ID = 12345
BUFF_SIZE = 4096
WAIT_EVENT_TIMEOUT = 30

HELLO_REPLY = '''\
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
    <capability>urn:ietf:params:netconf:base:1.1</capability>
  </capabilities>
  <session-id>{}</session-id>
</hello>
{}
'''.format(SESSION_ID, DELIM)

OK_REPLY_TEMPLATE = '''\
<rpc-reply
    xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"
    message-id="{}">
  <ok/>
</rpc-reply>
{}
'''


def make_rpc_reply(uuid):
    return OK_REPLY_TEMPLATE.format(uuid, DELIM)


class NetconfMockServer(paramiko.ServerInterface):
    worker_pool = gevent.pool.Pool()
    username = 'admin'
    password = 'admin'
    use_delays = False
    reply_delay_ranges = {'default': (0, 0)}

    @classmethod
    def reset(cls):
        cls.use_delays = False
        cls.reply_delay_ranges = {'default': (0, 0)}
        cls.fail_mode = False

    def __init__(self, ident):
        self.ident = ident
        self.event = threading.Event()
        self.channel = None
        self.data = ""
        self.session_closed = False

    def check_channel_request(self, kind, chanid):
        if kind == 'session':
            return paramiko.OPEN_SUCCEEDED
        return paramiko.OPEN_FAILED_ADMINISTRATIVELY_PROHIBITED

    def check_auth_password(self, username, password):
        if username == self.username and password == self.password:
            return paramiko.AUTH_SUCCESSFUL
        return paramiko.AUTH_FAILED

    def get_allowed_auths(self, username):
        return 'password'

    def check_channel_subsystem_request(self, channel, name):
        self.channel = channel
        self.event.set()
        return True

    def receive_data(self):
        idx = self.data.find(DELIM)
        while idx < 0:
            new_data = self.channel.recv(BUFF_SIZE)
            if new_data:
                logger.debug('received chunk:\n%s', new_data)
                self.data += new_data.decode()
                idx = self.data.find(DELIM)
            if idx < 0:
                sleep(0.1)
        data = self.data[:idx].strip()
        self.data = self.data[idx + len(DELIM):]
        return data

    def process_request(self, xml_data):
        root = etree.fromstring(xml_data.encode())
        uuid = root.attrib.get('message-id', 'unknown')
        request_type = etree.QName(root[0]).localname
        logger.debug('%s RPC received (message-id=%s rpc=%s)', self.ident, uuid, request_type)

        if request_type == "capabilities":
            return

        if request_type == "close-session":
            logger.info('%s close-session received', self.ident)
            self.session_closed = True
            reply = make_rpc_reply(uuid)
            self.channel.sendall(reply)
            return

        if self.use_delays:
            min_d, max_d = self.reply_delay_ranges.get(
                request_type, self.reply_delay_ranges['default'])
            delay = random.randint(min_d, max_d)
            logger.debug('%s delaying response %ds (range %d-%d)', self.ident, delay, min_d, max_d)
            sleep(delay)

        reply = make_rpc_reply(uuid)
        logger.debug('%s sending reply:\n%s', self.ident, reply)
        self.channel.sendall(reply)

    def handle_session(self):
        self.channel.sendall(HELLO_REPLY)
        while not self.session_closed:
            xml_data = self.receive_data()
            logger.debug('%s received:\n%s', self.ident, repr(xml_data))
            self.process_request(xml_data)


controller = Flask(__name__)


@controller.route('/')
def help():
    return jsonify({
        '/set_use_delays': 'Enable response delays',
        '/set_no_delays': 'Disable response delays',
        '/delays_range': 'POST {"delay": N} or {"min": N, "max": M} to set delay range',
        '/reset': 'Reset all state to defaults',
    })


@controller.route('/set_use_delays', methods=['GET', 'POST'])
def set_use_delays():
    NetconfMockServer.use_delays = True
    logger.info('Delays enabled')
    return "Ok"


@controller.route('/set_no_delays', methods=['GET', 'POST'])
def set_no_delays():
    NetconfMockServer.use_delays = False
    logger.info('Delays disabled')
    return "Ok"


@controller.route('/delays_range', methods=['POST'])
def delays_range():
    """Set delay range. POST {"delay": N} or {"min": N, "max": M} or {"<rpc>": {"delay": N}}"""
    data = request.get_json()
    logger.debug('/delays_range received: %s', data)

    if 'delay' in data:
        v = int(data['delay'])
        NetconfMockServer.reply_delay_ranges['default'] = (v, v)
    elif 'min' in data and 'max' in data:
        NetconfMockServer.reply_delay_ranges['default'] = (int(data['min']), int(data['max']))
    else:
        for rpc, spec in data.items():
            if 'delay' in spec:
                v = int(spec['delay'])
                NetconfMockServer.reply_delay_ranges[rpc] = (v, v)
            elif 'min' in spec and 'max' in spec:
                NetconfMockServer.reply_delay_ranges[rpc] = (int(spec['min']), int(spec['max']))

    logger.info('Delay ranges updated: %s', NetconfMockServer.reply_delay_ranges)
    return "Ok"


@controller.route('/reset', methods=['GET', 'POST'])
def reset():
    NetconfMockServer.reset()
    logger.info('Server state reset')
    return "Ok"


def handle_connection(transport, ident):
    try:
        server = NetconfMockServer(ident)
        try:
            transport.start_server(server=server)
        except paramiko.SSHException:
            logger.exception('%s SSH negotiation failed', ident)
            return

        chan = transport.accept(20)
        if chan is None:
            logger.error('%s No channel', ident)
            return
        logger.info('%s Authenticated', ident)

        server.event.wait(WAIT_EVENT_TIMEOUT)
        if not server.event.is_set():
            logger.error('%s Client did not request subsystem', ident)
            return

        try:
            server.handle_session()
        finally:
            logger.info('%s Closing channel', ident)
            server.channel.close()

    except Exception:
        logger.exception('Unhandled exception in connection handler')
        try:
            transport.close()
        except Exception:
            pass


def run_server(port, host_key):
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    sock.bind(('', port))
    logger.info('NETCONF mock listening on port %d (user=%s)', port, NetconfMockServer.username)

    pool = gevent.pool.Pool()
    while True:
        try:
            sock.listen(100)
            client, addr = sock.accept()
        except Exception:
            logger.exception('accept failed')
            continue

        logger.debug('Connection from %s', addr)
        t = paramiko.Transport(client)
        t.add_server_key(host_key)
        pool.spawn(handle_connection, t, str(addr))


def main():
    import argparse
    parser = argparse.ArgumentParser(description='NETCONF mock server')
    parser.add_argument('-P', '--port', type=int, default=830)
    parser.add_argument('--http-port', type=int, default=8088)
    parser.add_argument('--http-host', default='0.0.0.0')
    parser.add_argument('-u', '--user', default='admin')
    parser.add_argument('-p', '--password', default='admin')
    args = parser.parse_args()

    NetconfMockServer.username = args.user
    NetconfMockServer.password = args.password

    host_key = paramiko.RSAKey(filename='netconf_rsa.key')
    logger.debug('Host key fingerprint: %s', u(hexlify(host_key.get_fingerprint())))

    t1 = gevent.spawn(controller.run, port=args.http_port, host=args.http_host)
    t2 = gevent.spawn(run_server, args.port, host_key)
    try:
        gevent.joinall([t1, t2])
    except KeyboardInterrupt:
        pass


if __name__ == '__main__':
    main()
