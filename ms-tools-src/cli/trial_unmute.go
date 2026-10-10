package main
import("encoding/json";"fmt";"os";"time";"github.com/BertoldVdb/ms-tools/mshal")
type TrialUnmute struct {
 Journal string `arg help:"New JSONL journal file."`
 Seconds int `optional default:"12" help:"Trial duration, 1..30 seconds."`
 Hold bool `optional help:"Re-clear only if firmware reasserts the same bit; bounded duration."`
}
func(t TrialUnmute)Run(c *Context)(result error){
 if !CLI.NoPatch||CLI.NoFirmware||CLI.EEPROMSize!=2048{return fmt.Errorf("requires no-patch, eeprom-size=2048, firmware preserved")}
 if t.Seconds<1||t.Seconds>30{return fmt.Errorf("invalid duration")}
 f,e:=os.OpenFile(t.Journal,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0644);if e!=nil{return e};defer f.Close();enc:=json.NewEncoder(f)
 record:=func(event string,value byte){_ = enc.Encode(map[string]interface{}{"utc":time.Now().UTC().Format(time.RFC3339Nano),"event":event,"FEB9":fmt.Sprintf("%02x",value)});_ = f.Sync()}
 ram:=c.hal.MemoryRegionGet(mshal.MemoryRegionRAM)
 b,e:=readExact(ram,0xfeb9,1);if e!=nil{return e};original:=b[0];record("original",original)
 if original&1==0{return fmt.Errorf("FEB9 bit0 already clear; no write performed")}
 mshal.DiagnosticWriteRanges([][2]int{{0xfeb9,0xfeba}});defer mshal.DiagnosticWriteRanges(nil)
 defer func(){
  cur,e:=readExact(ram,0xfeb9,1);if e!=nil{result=fmt.Errorf("restore read failed: %v; original result %v",e,result);return}
  restored:=(cur[0]&0xfe)|(original&1)
  if e=writeVerify(ram,0xfeb9,[]byte{restored});e!=nil{result=fmt.Errorf("RESTORE FAILED: %v",e);record("restore_failed",restored)}else{record("restored_verified",restored);fmt.Printf("Restored FEB9 bit0; register=%02x\n",restored)}
 }()
 target:=original&0xfe
 if _,e=ram.Access(true,0xfeb9,[]byte{target});e!=nil{return e};record("clear_requested",target)
 deadline:=time.Now().Add(time.Duration(t.Seconds)*time.Second)
 for time.Now().Before(deadline){
  b,e:=readExact(ram,0xfeb9,1);if e!=nil{return e};record("sample",b[0])
  if t.Hold&&b[0]&1!=0{target=b[0]&0xfe;if _,e=ram.Access(true,0xfeb9,[]byte{target});e!=nil{return e};record("reclear_requested",target)}
  time.Sleep(200*time.Millisecond)
 }
 return nil
}
