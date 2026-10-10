package main

import (
 "bytes"
 "crypto/sha256"
 "fmt"
 "os"
 "path/filepath"
 "time"
 "github.com/BertoldVdb/ms-tools/mshal"
)

type SafeDumpROM struct { Directory string `arg help:"New directory for backups and two ROM reads."` }
type savedRAM struct { address int; data []byte; name string }

func exclusiveSave(path string, b []byte) error {
 f,e:=os.OpenFile(path,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0444); if e!=nil{return e}; defer f.Close()
 if _,e=f.Write(b);e!=nil{return e}; return f.Sync()
}
func readExact(r mshal.MemoryRegion,a,n int)([]byte,error){
 b:=make([]byte,n); got,e:=r.Access(false,a,b); if e!=nil{return nil,e};if got!=n{return nil,fmt.Errorf("short read at %04x",a)};return b,nil
}
func writeVerify(r mshal.MemoryRegion,a int,b []byte)error{
 n,e:=r.Access(true,a,b);if e!=nil{return e};if n!=len(b){return fmt.Errorf("short write at %04x",a)}
 rb,e:=readExact(r,a,len(b));if e!=nil{return e};if !bytes.Equal(b,rb){return fmt.Errorf("verify failed at %04x: got %x expected %x",a,rb,b)};return nil
}

func (d SafeDumpROM) Run(c *Context)(result error){
 if !CLI.NoPatch || CLI.NoFirmware || CLI.EEPROMSize!=2048 {return fmt.Errorf("requires no-patch, eeprom-size=2048, firmware preserved")}
 if c.hal.GetDeviceType()!="MS2107"{return fmt.Errorf("unexpected ROM dispatch profile")}
 if e:=os.Mkdir(d.Directory,0755);e!=nil{return e}
 ram:=c.hal.MemoryRegionGet(mshal.MemoryRegionRAM)
 hdr,e:=readExact(ram,0xc7d0,48);if e!=nil{return e}
 if !bytes.Equal(hdr[:4],[]byte{8,0x16,4,0xa7}) || hdr[8]!=3 {return fmt.Errorf("unexpected header/flags: %x",hdr)}
 code,e:=readExact(ram,0xc800,1191);if e!=nil{return e}
 if !bytes.Equal(code[:3],[]byte{2,0xc8,0x13}) || !bytes.Equal(code[16:19],[]byte{2,0xcb,0xf8}) {return fmt.Errorf("unexpected hook code")}
 if e=exclusiveSave(filepath.Join(d.Directory,"header-before.bin"),hdr);e!=nil{return e}
 if e=exclusiveSave(filepath.Join(d.Directory,"code-before.bin"),code);e!=nil{return e}
 blob:=bytes.ReplaceAll(dumpBlobBase,[]byte{0xde,0xad},[]byte{0xd0,0x00})
 saved:=[]savedRAM{{0xc800,code[:len(blob)],"hook"},{0xd000,nil,"mailbox"},{0xd100,nil,"scratch"}}
 saved[1].data,e=readExact(ram,0xd000,5);if e!=nil{return e}
 saved[2].data,e=readExact(ram,0xd100,255);if e!=nil{return e}
 for _,s:=range saved {if e=exclusiveSave(filepath.Join(d.Directory,s.name+"-before.bin"),s.data);e!=nil{return e}}
 time.Sleep(100*time.Millisecond)
 for _,s:=range saved {rb,e:=readExact(ram,s.address,len(s.data));if e!=nil{return e};if !bytes.Equal(rb,s.data){return fmt.Errorf("RAM %04x changed before experiment; abort",s.address)}}
 mshal.DiagnosticWriteRanges([][2]int{{0xc7d8,0xc7d9},{0xc800,0xc800+len(blob)},{0xd000,0xd005},{0xd100,0xd1ff}})
 defer mshal.DiagnosticWriteRanges(nil)
 defer func(){
   var failures []string
   if e:=writeVerify(ram,0xc7d8,[]byte{0});e!=nil{failures=append(failures,e.Error())}
   time.Sleep(time.Second)
   for _,s:=range saved{if e:=writeVerify(ram,s.address,s.data);e!=nil{failures=append(failures,e.Error())}}
   // Do not re-enable hooks if code restoration failed.
   if len(failures)==0 {if e:=writeVerify(ram,0xc7d8,[]byte{hdr[8]});e!=nil{failures=append(failures,e.Error())}}
   after,er:=readExact(ram,0xc800,len(code));if er!=nil{failures=append(failures,er.Error())}else if !bytes.Equal(after,code){failures=append(failures,"full code restore comparison failed")}
   if er==nil{_ = exclusiveSave(filepath.Join(d.Directory,"code-after.bin"),after)}
   hafter,er:=readExact(ram,0xc7d0,48);if er!=nil{failures=append(failures,er.Error())}else{_ = exclusiveSave(filepath.Join(d.Directory,"header-after.bin"),hafter);if !bytes.Equal(hafter,hdr){failures=append(failures,"header differs after restoration")}}
   status:=fmt.Sprintf("restoration failures: %v\n",failures);fmt.Print(status);_ = exclusiveSave(filepath.Join(d.Directory,"restore-status.txt"),[]byte(status))
   if len(failures)>0{result=fmt.Errorf("STOP: restoration failed: %v; original error %v",failures,result)}
 }()
 if e=writeVerify(ram,0xc7d8,[]byte{0});e!=nil{return e};time.Sleep(time.Second)
 if e=writeVerify(ram,0xd000,[]byte{0});e!=nil{return e}
 if e=writeVerify(ram,0xc800,blob);e!=nil{return e}
 if e=writeVerify(ram,0xc7d8,[]byte{1});e!=nil{return e}
 var first []byte
 for pass:=1;pass<=2;pass++ {
   image:=make([]byte,0x8000)
   for addr:=0;addr<len(image); {
     n:=255;if len(image)-addr<n{n=len(image)-addr}
     if e=writeVerify(ram,0xd001,[]byte{byte(addr>>8),byte(addr),0xd1,0});e!=nil{return e}
     // Mailbox is consumed asynchronously, so do not demand readback equality.
     if _,e=ram.Access(true,0xd000,[]byte{byte(n)});e!=nil{return e}
     deadline:=time.Now().Add(2*time.Second)
     for {rb,e:=readExact(ram,0xd000,1);if e!=nil{return e};if rb[0]==0{break};if time.Now().After(deadline){return fmt.Errorf("ROM mailbox timeout at %04x",addr)};time.Sleep(5*time.Millisecond)}
     b,e:=readExact(ram,0xd100,n);if e!=nil{return e};copy(image[addr:],b);addr+=n
     if addr%4080==0 || addr==len(image){fmt.Printf("ROM pass %d: %d/%d bytes\n",pass,addr,len(image))}
   }
   if e=exclusiveSave(filepath.Join(d.Directory,fmt.Sprintf("rom-read%d.bin",pass)),image);e!=nil{return e}
   fmt.Printf("ROM pass %d SHA256 %x\n",pass,sha256.Sum256(image))
   if pass==1{first=image}else if !bytes.Equal(first,image){return fmt.Errorf("ROM reads differ")}
 }
 return nil
}
