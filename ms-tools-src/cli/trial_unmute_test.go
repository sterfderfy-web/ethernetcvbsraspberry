package main
import("path/filepath";"testing";"github.com/BertoldVdb/ms-tools/mshal")
func TestUnmuteRestoresOnlySelectedBit(t *testing.T){
 old:=CLI;defer func(){CLI=old}();CLI.NoPatch=true;CLI.EEPROMSize=2048;CLI.NoFirmware=false
 d:=&memoryHID{};d.mem[0xf800]=0xff;d.mem[0xfeb9]=0xa5
 h,e:=mshal.New(d,mshal.HALConfig{EEPromSize:2048,LogFunc:func(int,string,...interface{}){}});if e!=nil{t.Fatal(e)}
 if e=(TrialUnmute{Journal:filepath.Join(t.TempDir(),"trial.jsonl"),Seconds:1}).Run(&Context{hal:h});e!=nil{t.Fatal(e)}
 if d.mem[0xfeb9]!=0xa5{t.Fatalf("not restored: %x",d.mem[0xfeb9])}
 if len(d.writes)!=2{t.Fatalf("unexpected write count: %d",len(d.writes))};for _,a:=range d.writes{if a!=0xfeb9{t.Fatalf("unexpected write %04x",a)}}
}
