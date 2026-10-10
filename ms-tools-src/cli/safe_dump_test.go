package main
import("bytes";"fmt";"os";"path/filepath";"testing";"github.com/BertoldVdb/ms-tools/mshal")
type memoryHID struct{ mem [65536]byte; last [9]byte; failScratch bool; failed bool; writes []int }
func(d *memoryHID)Close()error{return nil}
func(d *memoryHID)SendFeatureReport(b []byte)(int,error){copy(d.last[:],b);a:=int(b[2])<<8|int(b[3]);if b[1]==0xb6{d.writes=append(d.writes,a);d.mem[a]=b[4];if a==0xd000 && b[4]!=0 && d.mem[0xc7d8]==1 {src:=int(d.mem[0xd001])<<8|int(d.mem[0xd002]);for i:=0;i<int(b[4]);i++{d.mem[0xd100+i]=byte((src+i)*17)};d.mem[0xd000]=0}};return len(b),nil}
func(d *memoryHID)GetFeatureReport(b []byte)(int,error){copy(b,d.last[:]);a:=int(b[2])<<8|int(b[3]);if b[1]==0xb5{if d.failScratch && !d.failed && a==0xd100 && d.mem[0xc7d8]==1{d.failed=true;return 0,fmt.Errorf("injected read failure")};b[4]=d.mem[a]};return len(b),nil}
func TestSafeDumpRestoresOnSuccessAndFailure(t *testing.T){
 for _,fail:=range []bool{false,true}{t.Run(fmt.Sprint(fail),func(t *testing.T){
  old:=CLI;defer func(){CLI=old}();CLI.NoPatch=true;CLI.EEPROMSize=2048;CLI.NoFirmware=false
  d:=&memoryHID{failScratch:fail};d.mem[0xf800]=0xff;copy(d.mem[0xc7d0:],[]byte{8,0x16,4,0xa7});d.mem[0xc7d8]=3
  copy(d.mem[0xc800:],[]byte{2,0xc8,0x13});copy(d.mem[0xc810:],[]byte{2,0xcb,0xf8});for i:=0xd000;i<0xd200;i++{d.mem[i]=byte(i)}
  before:=d.mem
  h,e:=mshal.New(d,mshal.HALConfig{EEPromSize:2048,LogFunc:func(int,string,...interface{}){}});if e!=nil{t.Fatal(e)}
  dir:=filepath.Join(t.TempDir(),"dump");e=(SafeDumpROM{Directory:dir}).Run(&Context{hal:h})
  if (e!=nil)!=fail{t.Fatalf("error=%v fail=%v",e,fail)}
  if !bytes.Equal(before[:],d.mem[:]){t.Fatal("memory not restored")}
  for _,a:=range d.writes{if !(a==0xc7d8 || a>=0xc800&&a<0xc800+len(dumpBlobBase) || a>=0xd000&&a<0xd005 || a>=0xd100&&a<0xd1ff){t.Fatalf("unexpected write %04x",a)}}
  if !fail{a,_:=os.ReadFile(filepath.Join(dir,"rom-read1.bin"));b,_:=os.ReadFile(filepath.Join(dir,"rom-read2.bin"));if len(a)!=32768||!bytes.Equal(a,b){t.Fatal("ROM output mismatch")}}
 })}
}
