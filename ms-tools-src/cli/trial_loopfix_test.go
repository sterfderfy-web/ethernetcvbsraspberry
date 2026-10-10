package main
import("bytes";"fmt";"os";"path/filepath";"testing";"time";"github.com/BertoldVdb/ms-tools/mshal")
type loopHID struct{memoryHID;inject bool}
func(d *loopHID)GetFeatureReport(b []byte)(int,error){a:=int(d.last[2])<<8|int(d.last[3]);if d.inject&&!d.failed&&a==0xfb4d&&d.mem[0xca95]==0x22{d.failed=true;return 0,fmt.Errorf("injected sample read error")};return d.memoryHID.GetFeatureReport(b)}
func TestLoopfixRestores(t *testing.T){
 firmwarePath:=os.Getenv("MS_TOOLS_TEST_FIRMWARE");if firmwarePath==""{t.Skip("set MS_TOOLS_TEST_FIRMWARE to a local known-good RAM capture")};firmware,e:=os.ReadFile(firmwarePath);if e!=nil{t.Fatal(e)}
 oldDuration:=loopTrialDuration;loopTrialDuration=time.Millisecond;defer func(){loopTrialDuration=oldDuration}()
 for _,fail:=range []bool{false,true}{t.Run(fmt.Sprint(fail),func(t *testing.T){
 old:=CLI;defer func(){CLI=old}();CLI.NoPatch=true;CLI.NoFirmware=false;CLI.EEPROMSize=2048
 d:=&loopHID{inject:fail};d.mem[0xf800]=0xff;d.mem[0xfeb9]=0xa5;d.mem[0xfbd1]=0xc1;d.mem[0xc7d8]=3;copy(d.mem[0xc800:],firmware);before:=d.mem
 h,e:=mshal.New(d,mshal.HALConfig{EEPromSize:2048,LogFunc:func(int,string,...interface{}){}});if e!=nil{t.Fatal(e)}
 e=(TrialLoopfix{Directory:filepath.Join(t.TempDir(),"trial"),DecoderUnmute:true,FirmwareHold:true}).Run(&Context{hal:h});if (e!=nil)!=fail{t.Fatalf("error=%v fail=%v",e,fail)}
 if !bytes.Equal(before[:],d.mem[:]){t.Fatal("state not restored")};for _,a:=range d.writes{if !(a==0xca95||a==0xfeb9||a==0xfbd1||a==0xc7d8||a>=0xc800&&a<0xc803||a>=0xd100&&a<0xd180){t.Fatalf("out-of-bounds write %04x",a)}}
 })}
}
func TestHoldBlobInstructionsAndPreservation(t *testing.T){
 for _,gate:=range []byte{0,1}{
  b:=holdBlob(0);if len(b)>=126{t.Fatal("code overlaps markers")};regs:=[256]byte{};regs[0xe0]=0xa7;regs[0xd0]=0x81;regs[0x82]=0x34;regs[0x83]=0x12;expected:=regs
  mem:=map[int]byte{0xd17f:gate,0xfeb9:0xa5,0xfbd1:0xc1};stack:=[]byte{};calls:=0;pc:=0;steps:=0
  for pc<len(b)&&steps<100{steps++;op:=b[pc];pc++;dp:=int(regs[0x83])<<8|int(regs[0x82]);switch op{
   case 0xc0:a:=b[pc];pc++;stack=append(stack,regs[a])
   case 0xd0:a:=b[pc];pc++;if len(stack)==0{t.Fatal("stack underflow")};regs[a]=stack[len(stack)-1];stack=stack[:len(stack)-1]
   case 0x90:regs[0x83]=b[pc];regs[0x82]=b[pc+1];pc+=2
   case 0x74:regs[0xe0]=b[pc];pc++
   case 0xe4:regs[0xe0]=0
   case 0xe0:regs[0xe0]=mem[dp]
   case 0xf0:mem[dp]=regs[0xe0]
   case 0x54:regs[0xe0]&=b[pc];pc++
   case 0x60:rel:=int(int8(b[pc]));pc++;if regs[0xe0]==0{pc+=rel}
   case 0x12:if b[pc]!=0xc8||b[pc+1]!=0x13||regs!=expected||mem[0xd17e]!=1{t.Fatal("bad original handler call")};pc+=2;calls++;regs[0xe0]=0x36;regs[0x82]=0x78;regs[0x83]=0x56;regs[0xd0]=0x40;expected=regs
   case 0x22:pc=len(b)
   default:t.Fatalf("unsupported opcode %x",op)
  }}
  if calls!=1||len(stack)!=0||regs!=expected||mem[0xd17e]!=0{t.Fatal("state/stack/call preservation failed")}
  if gate==0{if mem[0xfbd1]!=0xc1||mem[0xfeb9]!=0xa5{t.Fatal("gate zero modified video")}}else if mem[0xfbd1]!=0||mem[0xfeb9]!=0xa4{t.Fatal("unmute wrong")}
 }
}
