#!/usr/bin/env python3
"""Execute the compiled shim under Unicorn with the game's IOP APIs mocked.

pip install unicorn; python3 patches/outrun2006/test_transport.py
No retail game code or ticket secrets are needed for these tests.
"""
import json
from pathlib import Path
import struct
import unittest
from unicorn import Uc, UC_ARCH_MIPS, UC_MODE_MIPS64, UC_MODE_LITTLE_ENDIAN, UC_HOOK_CODE
from unicorn.mips_const import UC_MIPS_REG_0, UC_MIPS_REG_PC
import build

STOP=0x10000
SERVER=bytes([192,0,2,7])
VIRTUAL=bytes([10,254,0,1])
OTHER=bytes([10,254,0,2])
TOKEN=bytes(range(1,17))
def integer(b):return int.from_bytes(b,'little')
def frame(kind,ip,payload=b'',token=TOKEN):return b'AOR1'+struct.pack('<I',kind)+token+ip+struct.pack('<I',len(payload))+payload
class Machine:
 def __init__(self):
  self.symbols=json.loads((build.BUILD/'symbols.json').read_text())
  self.uc=Uc(UC_ARCH_MIPS,UC_MODE_MIPS64|UC_MODE_LITTLE_ENDIAN)
  self.uc.mem_map(0,0x2000000)
  self.uc.mem_write(0x3bd000,(build.BUILD/'shim.bin').read_bytes())
  self.io_limit=build.RPC_PATCHES[0x13d3f4][1]&0xffff
  self.state=self.symbols['state'];self.sent=[];self.queue=[];self.created=0;self.originals=[];self.full={}
  self.uc.hook_add(UC_HOOK_CODE,self.step)
  self.write(0x533e8c,0x700000);self.write(0x700008,0x700100);self.write(0x70013c,0x700200)
  self.write(0x700208,0x700300);self.write(0x70020c,1);self.write(0x700210,1)
 def reg(self,n):return self.uc.reg_read(UC_MIPS_REG_0+n)
 def setreg(self,n,value):self.uc.reg_write(UC_MIPS_REG_0+n,value&0xffffffffffffffff)
 def write(self,address,*words):self.uc.mem_write(address,struct.pack('<'+'I'*len(words),*words))
 def word(self,address):return struct.unpack('<I',self.uc.mem_read(address,4))[0]
 def returns(self,value=0):self.setreg(2,value);self.uc.reg_write(UC_MIPS_REG_PC,self.reg(31))
 def step(self,uc,address,size,user):
  word=self.word(address);op=word>>26
  if address in (0x225a98,0x13ef60,0x13e918,0x13ea88) or address in [self.symbols['original_'+n] for n in ('send','receive','update','discovery','auth','create')]:
   assert self.reg(29)%16==0, 'EE call stack is not 16-byte aligned'
  # Emulate the EE-only 128-bit GPR save/restore operations. Unicorn exposes
  # the low 64 bits; a shadow bank verifies upper-half preservation too.
  if op in (0x1e,0x1f):
   base=(word>>21)&31;reg=(word>>16)&31;imm=word&65535;imm=imm-65536 if imm>=32768 else imm
   target=(self.reg(base)+imm)&0xffffffff
   if op==0x1f:uc.mem_write(target,struct.pack('<QQ',self.reg(reg),self.full.get(reg,0)))
   else:
    low,high=struct.unpack('<QQ',uc.mem_read(target,16));self.setreg(reg,low);self.full[reg]=high
   uc.reg_write(UC_MIPS_REG_PC,address+4);return
  if address==0x225a98:self.returns(7)
  elif address==0x13ef60:self.returns(bool(self.queue))
  elif address==0x13e918:
   assert self.reg(7)==0 and self.reg(9)==16
   addr=self.reg(8)&0xffffffff;length=self.reg(6)
   if length>self.io_limit:self.returns(-1);return
   self.sent.append((bytes(uc.mem_read(addr,16)),bytes(uc.mem_read(self.reg(5)&0xffffffff,length))))
   self.returns(length)
  elif address==0x13ea88:
   assert self.reg(7)==0 and self.reg(9)==self.reg(8)+16
   addr=self.reg(8)&0xffffffff
   if self.reg(6)>self.io_limit:self.returns(-1);return
   src,data=self.queue.pop(0)
   data=data[:self.reg(6)];uc.mem_write(self.reg(5)&0xffffffff,data)
   uc.mem_write(addr,struct.pack('<HH',2,0x4a0e)+src+bytes(8));self.returns(len(data))
  elif address==self.symbols['original_create']:self.created+=1;self.returns()
  else:
   for name in ('update','discovery','auth','send','receive'):
    if address==self.symbols['original_'+name]:self.originals.append(name);self.returns(700 if name=='auth' else 1);break
 def call(self,name,*args,hook=False):
  self.setreg(29,0x1ff0000);self.setreg(31,STOP)
  for i,value in enumerate(args):self.setreg(4+i,value)
  self.uc.emu_start(self.symbols[('hook_' if hook else 'shim_')+name],STOP,count=200000)
  assert self.uc.reg_read(UC_MIPS_REG_PC)==STOP,'execution did not return'
  return self.reg(2)
 def login(self):
  self.write(0x710000,0,integer(SERVER),0,0,0,1);self.call('discovery',0x710000)
  self.write(0x710118,0x710200);self.uc.mem_write(0x710210,b'ANGL'+TOKEN+bytes(108))
  self.call('auth',0x710100,0);self.write(0x374824,1)
 def packet(self,payload=b'\x03\x00\x17\x44\x55'):
  self.uc.mem_write(0x720100,payload);self.write(0x720000,0x720100,len(payload),1400,10)
  self.uc.mem_write(0x720200,OTHER+struct.pack('<HHI',3658,0,0))
  self.write(0x374810,0x730000);self.write(0x730064,0x730100);self.write(0x730100,0x1234)
 def ack(self):self.queue.append((SERVER,frame(3,VIRTUAL)));self.call('receive',0x720000,0,0)
class TransportTests(unittest.TestCase):
 @classmethod
 def setUpClass(cls):build.build()
 def test_automatic_host_registration_and_keepalive(self):
  m=Machine();m.login();m.packet();m.call('create',0);self.assertEqual(m.created,0)
  m.call('update',0);self.assertEqual(m.sent[0][1],frame(1,bytes(4)))
  self.assertEqual(m.sent[0][0][:8],struct.pack('<HH',0x0200,0x4a0e)+SERVER)
  m.ack();self.assertEqual(m.word(m.state+12),1)
  self.assertEqual(bytes(m.uc.mem_read(0x700214,6)),VIRTUAL+struct.pack('<H',3658))
  self.assertEqual(bytes(m.uc.mem_read(0x700300,6)),VIRTUAL+struct.pack('<H',3658))
  m.call('create',0);self.assertEqual(m.created,1)
  for _ in range(301):m.call('update',0)
  self.assertGreaterEqual(len(m.sent),2)
 def test_registration_ack_before_native_worker_exists(self):
  # Retail regression: no native update/receive is scheduled while the player
  # is only in the online menus. The global pump must finish registration.
  m=Machine();m.login();m.call('create',0);self.assertEqual(m.created,0)
  m.call('update');self.assertEqual(m.sent[-1][1],frame(1,bytes(4)))
  m.queue.append((SERVER,frame(3,VIRTUAL)));m.call('update')
  self.assertEqual(m.word(m.state+12),1)
  m.call('create',0);self.assertEqual(m.created,1)
 def test_network_pump_preserves_native_packet_order(self):
  m=Machine();m.login();m.packet();m.ack()
  first=b'\x12\x34\x12\x01';second=b'\xab\xcd\x17\x02'
  m.queue.extend([(SERVER,frame(4,OTHER,first)),(SERVER,frame(4,OTHER,second))])
  m.call('update');m.call('update')
  self.assertEqual(len(m.queue),1,'pump must stop at a queued native packet')
  self.assertEqual(m.call('receive',0x720000,0,0),1)
  self.assertEqual(bytes(m.uc.mem_read(0x720100,4)),b'\x02\x00'+first[2:])
  self.assertEqual(m.call('receive',0x720000,0,0),1)
  self.assertEqual(bytes(m.uc.mem_read(0x720100,4)),b'\x02\x00'+second[2:])
 def test_native_send_and_receive(self):
  m=Machine();m.login();m.packet();m.ack();before=bytes(m.uc.mem_read(0x720100,5))
  self.assertEqual(m.call('send',0x720000,0x720200,0),1)
  self.assertEqual(m.sent[-1][1],frame(2,OTHER,b'\x12\x34'+before[2:]))
  self.assertEqual(bytes(m.uc.mem_read(0x720100,5)),before)
  m.queue.append((SERVER,frame(4,OTHER,b'\xab\xcd\x17\x00\xff')))
  self.assertEqual(m.call('receive',0x720000,0,0x720200),1)
  self.assertEqual(bytes(m.uc.mem_read(0x720100,5)),b'\x03\x00\x17\x00\xff')
  self.assertEqual(bytes(m.uc.mem_read(0x720010,12)),OTHER+struct.pack('<HHI',3658,0xabcd,0))
  self.assertEqual(bytes(m.uc.mem_read(0x720200,12)),bytes(m.uc.mem_read(0x720010,12)))
 def test_malformed_wrong_server_and_small_buffer(self):
  m=Machine();m.login();m.packet()
  for src,data in [(OTHER,frame(3,VIRTUAL)),(SERVER,frame(3,VIRTUAL,token=bytes(16))),(SERVER,b'AOR1'),(SERVER,frame(3,OTHER)+b'X')]:
   m.queue.append((src,data));m.call('receive',0x720000,0,0)
  self.assertEqual(m.word(m.state+12),0)
  m.ack();m.write(0x720008,3);m.queue.append((SERVER,frame(4,OTHER,b'\x00\x00\x17\x01')))
  self.assertEqual(m.call('receive',0x720000,0,0),0)
 def test_offline_raw_and_bad_dest(self):
  m=Machine();m.packet();m.call('send',0x720000,0x720200,0);self.assertIn('send',m.originals)
  m.login();m.ack();m.call('receive',0x720000,1,0);self.assertIn('receive',m.originals)
  m.uc.mem_write(0x720200,SERVER);self.assertEqual(m.call('send',0x720000,0x720200,0),0)
 def test_max_native_packet_and_relogin(self):
  m=Machine();m.login();payload=b'\x00\x00'+bytes(range(256))*7+bytes(254)
  self.assertEqual(len(payload),2048)
  m.packet(payload);m.ack();self.assertEqual(m.call('send',0x720000,0x720200,0),1)
  self.assertEqual(len(m.sent[-1][1]),2080)
  m.write(0x720008,2048);m.queue.append((SERVER,frame(4,OTHER,payload)))
  m.call('update');self.assertEqual(m.call('receive',0x720000,0,0),1)
  self.assertEqual(m.word(0x720004),2048)
  self.assertEqual(bytes(m.uc.mem_read(0x720100,2048)),struct.pack('<H',2046)+payload[2:])
  m.write(0x720004,2049);self.assertEqual(m.call('send',0x720000,0x720200,0),0)
  m.uc.mem_write(0x710214,bytes(reversed(TOKEN)));m.call('auth',0x710100,0)
  self.assertEqual(m.word(m.state+12),0)
  m.call('create',0);self.assertEqual(m.created,0)
 def test_retail_rpc_limit_and_matching_allocation(self):
  # The retail API rejects an oversized receive *request* before consuming a
  # datagram, even when only a 32-byte ACK is waiting. Reproduce that failure.
  m=Machine();m.login();m.io_limit=2048
  m.queue.append((SERVER,frame(3,VIRTUAL)));m.call('update')
  self.assertEqual(m.word(m.state+12),0)
  self.assertEqual(len(m.queue),1)
  # Apply the PNACH's constructor capacity, then the same ACK is readable.
  limit=build.RPC_PATCHES[0x13d3f4][1]&0xffff
  allocation=build.RPC_PATCHES[0x13d408][1]&0xffff
  self.assertGreaterEqual(limit,32+2048+1)
  self.assertGreaterEqual(allocation,limit+127)
  rpc_allocation=(limit+99)&~63
  self.assertEqual(rpc_allocation%64,0)
  self.assertGreaterEqual(rpc_allocation,limit+36)
  m.io_limit=limit;m.call('update');self.assertEqual(m.word(m.state+12),1)
 def test_ee_hook_register_preservation(self):
  m=Machine();m.login();m.packet();m.ack()
  for n in range(1,32):
   if n not in (2,4,5,6,29,31):m.setreg(n,(n<<40)+n)
   m.full[n]=0x1234000000000000+n
  before={n:m.reg(n) for n in range(1,32) if n not in (2,4,5,6,29,31)};upper=dict(m.full)
  self.assertEqual(m.call('send',0x720000,0x720200,0,hook=True),1)
  for n,value in before.items():self.assertEqual(m.reg(n),value,f'register {n}')
  for n,value in upper.items():
   if n not in (2,29):self.assertEqual(m.full[n],value,f'upper register {n}')
if __name__=='__main__':unittest.main()
