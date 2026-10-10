package main
import("bytes";"crypto/sha256";"encoding/json";"fmt";"os";"path/filepath";"time";"github.com/BertoldVdb/ms-tools/mshal")
type TraceState struct{Directory string `arg`; USB bool `optional help:"Trace USB command hook instead of common hook."`; Stack bool `optional help:"Also snapshot low IRAM stack 56..65."`}
// The trampoline runs BEFORE the original C813 handler. It preserves PSW,
// ACC and both DPTR bytes, uses no R0..R7 temporaries and tail-calls C813.
// Snapshot: counter, hook argument R7, IRAM 40,43,44,46,47,48,49,23, SP.
func traceBlob(usb,stack bool)[]byte{
 b:=[]byte{0xc0,0xd0,0xc0,0xe0,0xc0,0x82,0xc0,0x83,0x90,0xd1,0x80,0xe0,0x04,0xf0,0xa3,0xef,0xf0}
 fields:=[]byte{0x40,0x43,0x44,0x3b,0x47,0x48,0x49,0x23,0x81};if stack{for a:=byte(0x56);a<=0x65;a++{fields=append(fields,a)}};for _,a:=range fields{b=append(b,0xa3,0xe5,a,0xf0)}
 target:=[]byte{0xc8,0x13};if usb{target=[]byte{0xcb,0xf8}};b=append(b,0xd0,0x83,0xd0,0x82,0xd0,0xe0,0xd0,0xd0,0x02);return append(b,target...)
}
func(t TraceState)Run(c *Context)(result error){
 if !CLI.NoPatch||CLI.NoFirmware||CLI.EEPROMSize!=2048{return fmt.Errorf("requires original firmware/no-patch/2048")}
 if e:=os.Mkdir(t.Directory,0755);e!=nil{return e}
 ram:=c.hal.MemoryRegionGet(mshal.MemoryRegionRAM);hook:=0xc800;off:=0;disabled:=byte(2);if t.USB{hook=0xc810;off=16;disabled=1}
 code,e:=readExact(ram,0xc800,1191);if e!=nil{return e}
 if fmt.Sprintf("%x",sha256.Sum256(code))!="40a78e46954c9050a72896f44febd26dcf8559bd659a4489c4feb792936c4a10"{return fmt.Errorf("unknown running firmware")}
 flags,e:=readExact(ram,0xc7d8,1);if e!=nil{return e};if flags[0]!=3{return fmt.Errorf("unexpected flags")}
 dataSize:=11;if t.Stack{dataSize=27};scratch,e:=readExact(ram,0xd100,0x80+dataSize);if e!=nil{return e}
 for name,b:=range map[string][]byte{"code-before.bin":code,"scratch-before.bin":scratch,"flags-before.bin":flags,"trace-code.bin":traceBlob(t.USB,t.Stack)}{if e=exclusiveSave(filepath.Join(t.Directory,name),b);e!=nil{return e}}
 time.Sleep(100*time.Millisecond);rb,e:=readExact(ram,0xd100,len(scratch));if e!=nil{return e};if !bytes.Equal(scratch,rb){return fmt.Errorf("scratch is active; stop")}
 f,e:=os.OpenFile(filepath.Join(t.Directory,"trace.jsonl"),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0644);if e!=nil{return e};defer f.Close();enc:=json.NewEncoder(f)
 mshal.DiagnosticWriteRanges([][2]int{{0xc7d8,0xc7d9},{hook,hook+3},{0xd100,0xd180+dataSize}});defer mshal.DiagnosticWriteRanges(nil)
 defer func(){
  var errs []string
  if e:=writeVerify(ram,0xc7d8,[]byte{disabled});e!=nil{errs=append(errs,e.Error())}
  time.Sleep(100*time.Millisecond)
  if e:=writeVerify(ram,hook,code[off:off+3]);e!=nil{errs=append(errs,e.Error())}
  if e:=writeVerify(ram,0xd100,scratch);e!=nil{errs=append(errs,e.Error())}
  after,e:=readExact(ram,0xc800,len(code));if e!=nil||!bytes.Equal(after,code){errs=append(errs,"code comparison failed")}
  if len(errs)==0{if e:=writeVerify(ram,0xc7d8,flags);e!=nil{errs=append(errs,e.Error())}}
  status:=fmt.Sprintf("restoration failures: %v\n",errs);_ = exclusiveSave(filepath.Join(t.Directory,"restore-status.txt"),[]byte(status));fmt.Print(status)
  if len(errs)>0{result=fmt.Errorf("restoration failed: %v; prior error: %v",errs,result)}
 }()
 if e=writeVerify(ram,0xc7d8,[]byte{disabled});e!=nil{return e};time.Sleep(100*time.Millisecond)
 if e=writeVerify(ram,0xd100,traceBlob(t.USB,t.Stack));e!=nil{return e}
 if e=writeVerify(ram,0xd180,make([]byte,dataSize));e!=nil{return e}
 if e=writeVerify(ram,hook,[]byte{2,0xd1,0});e!=nil{return e}
 if e=writeVerify(ram,0xc7d8,flags);e!=nil{return e}
 for i:=0;i<32;i++{rb,e=readExact(ram,0xd180,dataSize);if e!=nil{return e};row:=map[string]interface{}{"utc":time.Now().UTC().Format(time.RFC3339Nano),"bytes":fmt.Sprintf("%x",rb)};if e=enc.Encode(row);e!=nil{return e};_ = f.Sync();fmt.Printf("trace %x\n",rb);time.Sleep(250*time.Millisecond)}
 return nil
}
