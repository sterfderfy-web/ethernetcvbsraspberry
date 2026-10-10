package main
import("bytes";"fmt";"os";"path/filepath";"testing";"github.com/BertoldVdb/ms-tools/mshal")
type traceHID struct{memoryHID;inject bool}
func(d *traceHID)GetFeatureReport(b []byte)(int,error){
 a:=int(d.last[2])<<8|int(d.last[3]);if d.inject&&!d.failed&&a==0xd180&&d.mem[0xc801]==0xd1&&d.mem[0xc7d8]==3{d.failed=true;return 0,fmt.Errorf("injected trace read error")};return d.memoryHID.GetFeatureReport(b)
}
func TestTraceRestore(t *testing.T){
 firmwarePath:=os.Getenv("MS_TOOLS_TEST_FIRMWARE");if firmwarePath==""{t.Skip("set MS_TOOLS_TEST_FIRMWARE to a local known-good RAM capture")};firmware,e:=os.ReadFile(firmwarePath);if e!=nil{t.Fatal(e)}
 for _,fail:=range []bool{false,true}{t.Run(fmt.Sprint(fail),func(t *testing.T){
 old:=CLI;defer func(){CLI=old}();CLI.NoPatch=true;CLI.NoFirmware=false;CLI.EEPROMSize=2048
 d:=&traceHID{inject:fail};d.mem[0xf800]=0xff;d.mem[0xc7d8]=3;copy(d.mem[0xc800:],firmware);for i:=0xd100;i<0xd18b;i++{d.mem[i]=byte(i)};before:=d.mem
 h,e:=mshal.New(d,mshal.HALConfig{EEPromSize:2048,LogFunc:func(int,string,...interface{}){}});if e!=nil{t.Fatal(e)}
 e=(TraceState{Directory:filepath.Join(t.TempDir(),"trace")}).Run(&Context{hal:h});if (e!=nil)!=fail{t.Fatalf("error=%v fail=%v",e,fail)}
 if !bytes.Equal(before[:],d.mem[:]){t.Fatal("RAM not restored")}
 for _,a:=range d.writes{if !(a==0xc7d8||a>=0xc800&&a<0xc803||a>=0xd100&&a<0xd18b){t.Fatalf("write out of bounds %x",a)}}
 })}
}
