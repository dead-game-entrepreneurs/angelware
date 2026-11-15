/* AngelWare native UDP shim, SLUS-21274 / 38C16493 only.
 * All addresses below are verified against the supported ELF by build.py.
 * The original game and emulator executables are never changed on disk.
 */
typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned int u32;
typedef int s32;
#define WORD(a) (*(volatile u32 *)(a))
#define HALF(a) (*(volatile u16 *)(a))
#define ONLINE (WORD(0x374824) != 0)
#define MAX_PAYLOAD 2048
#define HEADER 32
#define MAGIC 0x31524f41u /* AOR1 */
#define VIRTUAL(ip) (((ip)&0xffffu)==0xfe0au)
struct Address {u32 ip;u16 port,tag;u32 alternate;};
struct Packet {u8 *data;u32 length,capacity,cursor;struct Address source;};
struct Sockaddr {u16 family,port;u32 ip;u8 padding[8];u32 length;};
struct State {
 u32 server,virtual_ip,authenticated,ready,ticks,last_ack;
 u8 token[16];
 u32 registers,acks,sent,received,dropped;
 u32 pending_size;
 struct Address pending_source;
 u8 pending[MAX_PAYLOAD];
 u32 read_errors;
};
/* This is zeroed by ELF loading, never by a repeating PNACH write. */
volatile struct State state;
extern int original_send(struct Packet *,struct Address *,int);
extern int original_receive(struct Packet *,int,struct Address *);
extern void original_update(void);
extern void original_discovery(void *);
extern int original_auth(void *,void *);
extern void original_create(void *);
extern int send_raw(int,const void *,int,const struct Sockaddr *);
extern int receive_raw(int,void *,int,struct Sockaddr *);
#define GET_SOCKET ((int (*)(int))0x225a98)
#define POLL ((int (*)(void *,int,int))0x13ef60)
static void copy(void *to,const void *from,u32 n) {u8 *d=to;const u8*s=from;while(n--)*d++=*s++;}
static int equal(const void*a,const void*b,u32 n) {const u8*x=a,*y=b;while(n--)if(*x++!=*y++)return 0;return 1;}
static int pointer(u32 p,u32 n) {return p>=0x100000 && p<=0x2000000-n;}
static struct Sockaddr server_address(void) {
 struct Sockaddr a={0x0200,0x4a0e,0,{0},16}; /* htons(3658) */
 a.ip=state.server;return a;
}
static void header(u8 *buf,u32 kind,u32 ip,u32 size) {
 ((u32*)buf)[0]=MAGIC;((u32*)buf)[1]=kind;
 copy(buf+8,(const void*)state.token,16);((u32*)buf)[6]=ip;((u32*)buf)[7]=size;
}
static int enabled(void) {return ONLINE && state.authenticated && state.server;}
/* SDK CommonAddr: refcount, vtable, {local array, capacity, count}, public
 * bdAddr, NAT type. Preserve its allocation and metadata. */
static int advertise(void) {
 u32 manager=WORD(0x533e8c);if(!pointer(manager,12))return 0;
 u32 network=WORD(manager+8);if(!pointer(network,64))return 0;
 u32 common=WORD(network+0x3c);if(!pointer(common,40))return 0;
 u32 locals=WORD(common+8),count=WORD(common+16),capacity=WORD(common+12);
 if(count<1 || count>16 || count>capacity || !pointer(locals,count*8))return 0;
 for(u32 i=0;i<count;i++) {WORD(locals+i*8)=state.virtual_ip;HALF(locals+i*8+4)=3658;}
 WORD(common+20)=state.virtual_ip;HALF(common+24)=3658;
 return 1;
}
void shim_discovery(void *discovery) {
 original_discovery(discovery);
 /* A discovery object can already be complete on entry. */
 if(WORD((u32)discovery+20)==1) {
  u32 ip=WORD((u32)discovery+4);
  if(ip && !VIRTUAL(ip))state.server=ip;
 }
}
int shim_auth(void *auth,void *reply) {
 int result=original_auth(auth,reply);
 if(result==700) {
  u32 user=WORD((u32)auth+0x18);
  if(pointer(user,0xa8) && WORD(user+0x10)==0x4c474e41u) {
   if(!state.authenticated || !equal((const void*)state.token,(void*)(user+0x14),16)) {
    copy((void*)state.token,(void*)(user+0x14),16);
    state.ready=0;state.virtual_ip=0;state.ticks=0;state.last_ack=0;state.pending_size=0;
   }
   state.authenticated=1;
  }
 }
 return result;
}
static int receive_packet(struct Packet *,struct Address *);
void shim_update(void) {
 if(enabled()) {
  u32 tick=state.ticks++;
  if(state.ready && tick-state.last_ack>900) {state.ready=0;state.pending_size=0;}
  if((!state.ready && tick%30==0) || (state.ready && tick%300==0)) {
   int fd=GET_SOCKET(0);
   if(fd>=0) {u32 buf[8];struct Sockaddr addr=server_address();header((u8*)buf,1,0,0);send_raw(fd,buf,32,&addr);state.registers++;}
  }
  /* The global network pump runs before any host/search worker exists.
   * Consume registration ACKs here; preserve one native packet for its normal
   * game receiver if the central pump reaches us before the native worker. */
  receive_packet((struct Packet*)0,(struct Address*)0);
 }
 original_update();
}
void shim_create(void *worker) {
 /* The stock create controller is polled each frame, with its normal timeout. */
 if(enabled() && (!state.ready || !advertise()))return;
 original_create(worker);
}
int shim_send(struct Packet *packet,struct Address *dest,int raw) {
 if(!enabled() || raw || !dest)return original_send(packet,dest,raw);
 if(!state.ready || !VIRTUAL(dest->ip) || packet->length<3 || packet->length>MAX_PAYLOAD) {state.dropped++;return 0;}
 u8 buf[HEADER+MAX_PAYLOAD] __attribute__((aligned(16)));
 header(buf,2,dest->ip,packet->length);copy(buf+HEADER,packet->data,packet->length);
 u32 context=WORD(0x374810),self=pointer(context,104)?WORD(context+100):0;
 u16 tag=pointer(self,2)?HALF(self):0;buf[HEADER]=(u8)(tag>>8);buf[HEADER+1]=(u8)tag;
 struct Sockaddr addr=server_address();int fd=GET_SOCKET(0);if(fd<0)return 0;
 int sent=send_raw(fd,buf,HEADER+packet->length,&addr);if(sent==(int)(HEADER+packet->length)) {state.sent++;return 1;}return 0;
}
static int deliver(struct Packet *packet,struct Address *out,const u8 *data,u32 size,struct Address source) {
 if(size>packet->capacity) {state.dropped++;return 0;}
 copy(packet->data,data,size);*(u16*)packet->data=(u16)(size-2);
 packet->length=size;packet->cursor=0;packet->source=source;if(out)*out=source;
 state.received++;return 1;
}
static int receive_packet(struct Packet *packet,struct Address *out) {
 if(state.pending_size) {
  if(!packet)return 0;
  u32 size=state.pending_size;struct Address source;
  copy(&source,(const void*)&state.pending_source,sizeof(source));state.pending_size=0;
  return deliver(packet,out,(const u8*)state.pending,size,source);
 }
 int fd=GET_SOCKET(0);if(fd<0)return 0;
 /* Drain control and malformed packets without presenting them to the game.
  * Bound the loop so hostile traffic cannot monopolize a game frame. */
 for(int tries=0;tries<8;tries++) {
  struct {s32 fd;u16 events,revents;} poll={fd,1,0};
  if(POLL(&poll,1,0)<=0)return 0;
  u8 buf[HEADER+MAX_PAYLOAD+1] __attribute__((aligned(16)));
  struct Sockaddr addr={0,0,0,{0},16};
  int n=receive_raw(fd,buf,sizeof(buf),&addr);
  if(n<0) {state.read_errors++;return 0;}
  if(n<HEADER || n>HEADER+MAX_PAYLOAD || addr.ip!=state.server || addr.port!=0x4a0e || ((u32*)buf)[0]!=MAGIC || !equal(buf+8,(const void*)state.token,16) || ((u32*)buf)[7]!=(u32)(n-HEADER)) {state.dropped++;continue;}
  u32 kind=((u32*)buf)[1],ip=((u32*)buf)[6];
  if(!VIRTUAL(ip) || !(ip>>16)) {state.dropped++;continue;}
  if(kind==3 && n==HEADER) {state.virtual_ip=ip;state.last_ack=state.ticks;state.ready=advertise();state.acks++;continue;}
  if(kind!=4 || !state.ready || n<HEADER+3) {state.dropped++;continue;}
  u32 size=n-HEADER;u16 tag=((u16)buf[HEADER]<<8)|buf[HEADER+1];
  struct Address source={ip,3658,tag,0};
  if(packet)return deliver(packet,out,buf+HEADER,size,source);
  copy((void*)state.pending,buf+HEADER,size);
  copy((void*)&state.pending_source,&source,sizeof(source));state.pending_size=size;
  return 0;
 }
 return 0;
}
int shim_receive(struct Packet *packet,int raw,struct Address *out) {
 if(!enabled() || raw)return original_receive(packet,raw,out);
 return receive_packet(packet,out);
}
