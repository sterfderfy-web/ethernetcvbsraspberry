package main
import("bytes";"crypto/sha256";"encoding/json";"fmt";"os";"path/filepath";"time";"github.com/BertoldVdb/ms-tools/mshal")
type TrialLoopfix struct{Directory string `arg`; ParentPID int `optional help:"Restore when this owning patcher process exits (Windows)."`; Seconds int `optional default:"45" help:"Bounded duration 30..180 seconds, or up to 28800 in live mode."`; Live bool `optional help:"Keep unmute after warmup until stop file or duration."`; StopFile string `optional help:"Creating this file requests verified restoration."`; DecoderUnmute bool `optional help:"Also use ROM normal-video FBD1=00 during unmute phase."`; FirmwareHold bool `optional help:"Apply unmute after each common firmware hook, gated by D17F."`; DecoderMask bool `optional help:"Experiment: clear only FBD1 bit0, preserve upper mode bits."`; DecoderHigh bool `optional help:"Experiment: clear only FBD1 bit7, preserve timing bit0 and never enable random bit2."`; Decoder41 bool `optional help:"Experiment: fixed FBD1=41 derived from C1 minus bit7; random bit2 stays clear."`}
var loopTrialDuration=45*time.Second
func holdBlob(mask byte)[]byte{
 push:=[]byte{0xc0,0xd0,0xc0,0xe0,0xc0,0x82,0xc0,0x83};pop:=[]byte{0xd0,0x83,0xd0,0x82,0xd0,0xe0,0xd0,0xd0}
 b:=append([]byte{},push...);b=append(b,0x90,0xd1,0x7e,0x74,1,0xf0);b=append(b,pop...);b=append(b,0x12,0xc8,0x13);b=append(b,push...)
 decoder:=[]byte{0x90,0xfb,0xd1,0xe4,0xf0};if mask!=0{decoder=[]byte{0x90,0xfb,0xd1,0xe0,0x54,mask,0xf0}};if mask==0x41{decoder=[]byte{0x90,0xfb,0xd1,0x74,0x41,0xf0}};b=append(b,0x90,0xd1,0x7f,0xe0,0x60,byte(len(decoder)+7));b=append(b,decoder...);b=append(b,0x90,0xfe,0xb9,0xe0,0x54,0xfe,0xf0)
 b=append(b,0x90,0xd1,0x7e,0xe4,0xf0);b=append(b,pop...);return append(b,0x22)
}
func(t TrialLoopfix)Run(c *Context)(result error){
 if t.ParentPID>0{if t.StopFile==""{return fmt.Errorf("parent-pid requires stop-file")};cancel,e:=watchPatcherParent(t.ParentPID,t.StopFile);if e!=nil{return e};defer cancel()}
 limit:=180;if t.Live{limit=28800;if !t.FirmwareHold||!t.DecoderUnmute||t.StopFile==""{return fmt.Errorf("live requires firmware-hold, decoder-unmute, stop-file")}};duration:=loopTrialDuration;if t.Seconds!=0{if t.Seconds<30||t.Seconds>limit{return fmt.Errorf("duration out of range")};duration=time.Duration(t.Seconds)*time.Second}
 mask:=byte(0);if t.DecoderMask{mask=0xfe};if t.DecoderHigh{mask=0x7f};if t.Decoder41{mask=0x41};if t.DecoderMask&&t.DecoderHigh{return fmt.Errorf("choose only one decoder mask")};if t.FirmwareHold&&!t.DecoderUnmute{return fmt.Errorf("firmware-hold requires decoder-unmute")}
 if !CLI.NoPatch||CLI.NoFirmware||CLI.EEPROMSize!=2048{return fmt.Errorf("requires no-patch/original firmware/2048")}
 if e:=os.Mkdir(t.Directory,0755);e!=nil{return e}
 ram:=c.hal.MemoryRegionGet(mshal.MemoryRegionRAM);code,e:=readExact(ram,0xc800,1191);if e!=nil{return e}
 if fmt.Sprintf("%x",sha256.Sum256(code))!="40a78e46954c9050a72896f44febd26dcf8559bd659a4489c4feb792936c4a10"{return fmt.Errorf("unknown running firmware")}
 mute,e:=readExact(ram,0xfeb9,1);if e!=nil{return e}
 decoder,e:=readExact(ram,0xfbd1,1);if e!=nil{return e};if t.DecoderUnmute&&decoder[0]!=0&&decoder[0]!=0xc1&&decoder[0]!=0xc3{return fmt.Errorf("unexpected FBD1 %x",decoder)}
 if e=exclusiveSave(filepath.Join(t.Directory,"code-before.bin"),code);e!=nil{return e};if e=exclusiveSave(filepath.Join(t.Directory,"feb9-before.bin"),mute);e!=nil{return e}
 if e=exclusiveSave(filepath.Join(t.Directory,"fbd1-before.bin"),decoder);e!=nil{return e}
 var scratch,flags []byte
 if t.FirmwareHold{
  flags,e=readExact(ram,0xc7d8,1);if e!=nil{return e};if flags[0]!=3{return fmt.Errorf("unexpected hook flags")}
  scratch,e=readExact(ram,0xd100,128);if e!=nil{return e};time.Sleep(100*time.Millisecond);v,e:=readExact(ram,0xd100,128);if e!=nil{return e};if !bytes.Equal(v,scratch){return fmt.Errorf("scratch active")}
  if e=exclusiveSave(filepath.Join(t.Directory,"scratch-before.bin"),scratch);e!=nil{return e};if e=exclusiveSave(filepath.Join(t.Directory,"hold-code.bin"),holdBlob(mask));e!=nil{return e}
 }
 f,e:=os.OpenFile(filepath.Join(t.Directory,"events.jsonl"),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0644);if e!=nil{return e};defer f.Close();enc:=json.NewEncoder(f)
 record:=func(event string,values interface{}){_ = enc.Encode(map[string]interface{}{"utc":time.Now().UTC().Format(time.RFC3339Nano),"event":event,"values":values});_ = f.Sync()}
 ranges:=[][2]int{{0xca95,0xca96},{0xfeb9,0xfeba}};if t.DecoderUnmute{ranges=append(ranges,[2]int{0xfbd1,0xfbd2})};if t.FirmwareHold{ranges=append(ranges,[2]int{0xc800,0xc803},[2]int{0xc7d8,0xc7d9},[2]int{0xd100,0xd180})};mshal.DiagnosticWriteRanges(ranges);defer mshal.DiagnosticWriteRanges(nil)
 defer func(){
  var errs []string
  if t.FirmwareHold{
   if e:=writeVerify(ram,0xd17f,[]byte{0});e!=nil{errs=append(errs,e.Error())}
   if e:=writeVerify(ram,0xc7d8,[]byte{2});e!=nil{errs=append(errs,e.Error())}
   idle:=false;deadline:=time.Now().Add(5*time.Second);for time.Now().Before(deadline){v,e:=readExact(ram,0xd17e,1);if e!=nil{errs=append(errs,e.Error());break};if v[0]==0{idle=true;break};time.Sleep(25*time.Millisecond)}
   if !idle||len(errs)>0{result=fmt.Errorf("STOP: hook not safely quiescent; preserve trampoline and loopfix; errors %v",errs);record("restore_failed",result.Error());return}
   if e:=writeVerify(ram,0xc800,code[:3]);e!=nil{errs=append(errs,e.Error())}
   if e:=writeVerify(ram,0xd100,scratch);e!=nil{errs=append(errs,e.Error())}
  }
  if e:=writeVerify(ram,0xca95,code[0x295:0x296]);e!=nil{errs=append(errs,e.Error())}
  if t.DecoderUnmute{if e:=writeVerify(ram,0xfbd1,decoder);e!=nil{errs=append(errs,e.Error())}}
  v,e:=readExact(ram,0xfeb9,1);if e!=nil{errs=append(errs,e.Error())}else if e=writeVerify(ram,0xfeb9,[]byte{v[0]&0xfe|mute[0]&1});e!=nil{errs=append(errs,e.Error())}
  after,e:=readExact(ram,0xc800,len(code));if e!=nil||!bytes.Equal(after,code){errs=append(errs,"full code comparison failed")};if e==nil{_ = exclusiveSave(filepath.Join(t.Directory,"code-after.bin"),after)}
  if t.FirmwareHold&&len(errs)==0{if e:=writeVerify(ram,0xc7d8,flags);e!=nil{errs=append(errs,e.Error())}}
  record("restore",errs);status:=fmt.Sprintf("restoration failures: %v\n",errs);fmt.Print(status);_ = exclusiveSave(filepath.Join(t.Directory,"restore-status.txt"),[]byte(status));if len(errs)>0{result=fmt.Errorf("restore failed: %v; prior %v",errs,result)}
 }()
 // IRQ HID writes one complete opcode. Entry at CA95 is an instruction boundary;
 // existing backward branches reach it. RET returns to CB75, whose successor
 // CB7A reloads DPTR and does not use the calibration function's return registers.
 if t.FirmwareHold{
  if e=writeVerify(ram,0xc7d8,[]byte{2});e!=nil{return e}
  if e=writeVerify(ram,0xd100,holdBlob(mask));e!=nil{return e};if e=writeVerify(ram,0xd17e,[]byte{0,0});e!=nil{return e}
  if e=writeVerify(ram,0xc800,[]byte{2,0xd1,0});e!=nil{return e}
 }
 if e=writeVerify(ram,0xca95,[]byte{0x22});e!=nil{return e}
 if t.FirmwareHold{if e=writeVerify(ram,0xc7d8,flags);e!=nil{return e}}
 record("patch_verified",map[string]interface{}{"CA95":"90->22","firmware_hold":t.FirmwareHold})
 start:=time.Now();phase:="stock"
 for time.Since(start)<duration{
  if t.StopFile!=""{if _,e:=os.Stat(t.StopFile);e==nil{record("stop_file",t.StopFile);break}else if !os.IsNotExist(e){return e}};elapsed:=time.Since(start).Seconds();endUnmute:=duration.Seconds()-17;if t.Live{endUnmute=duration.Seconds()};targetPhase:="stock";if elapsed>=8&&elapsed<endUnmute{targetPhase="unmute"}else if elapsed>=endUnmute{targetPhase="restored_mute"}
  if t.FirmwareHold&&targetPhase!=phase{gate:=byte(0);if targetPhase=="unmute"{gate=1};if e=writeVerify(ram,0xd17f,[]byte{gate});e!=nil{return e};record("gate_verified",gate)}
  v,e:=readExact(ram,0xfeb9,1);if e!=nil{return e}
  if t.DecoderUnmute&&targetPhase=="unmute"{d,e:=readExact(ram,0xfbd1,1);if e!=nil{return e};target:=d[0]&mask;if t.Decoder41{target=0x41};if d[0]!=target{if e=writeVerify(ram,0xfbd1,[]byte{target});e!=nil{return e};record("decoder_clear_verified",d[0])}}
  if t.DecoderUnmute&&targetPhase=="restored_mute"&&phase!=targetPhase{if e=writeVerify(ram,0xfbd1,decoder);e!=nil{return e};record("decoder_restore_verified",decoder[0])}
  if targetPhase=="unmute"&&v[0]&1!=0{if e=writeVerify(ram,0xfeb9,[]byte{v[0]&0xfe});e!=nil{return e};record("clear_verified",v[0]&0xfe)}
  if targetPhase=="restored_mute"&&phase!=targetPhase{if e=writeVerify(ram,0xfeb9,[]byte{v[0]&0xfe|mute[0]&1});e!=nil{return e};record("mute_restore_verified",mute[0]&1)}
  phase=targetPhase
  regs:=map[string]interface{}{"phase":phase,"elapsed":elapsed}
  for _,a:=range []int{0xfeb9,0xfbd1,0xfb4d,0xfb4f,0xfb8d,0xc66b}{b,e:=readExact(ram,a,1);if e!=nil{return e};regs[fmt.Sprintf("%04x",a)]=fmt.Sprintf("%02x",b[0])};record("sample",regs)
  if t.Live{time.Sleep(time.Second)}else{time.Sleep(250*time.Millisecond)}
 }
 return nil
}
