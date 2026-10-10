package main

import("os";"syscall")

// Keep a process handle, not just a PID: PID reuse cannot prolong the patch.
func watchPatcherParent(pid int,stop string)(func(),error){
 h,e:=syscall.OpenProcess(0x00100000,false,uint32(pid));if e!=nil{return nil,e}
 done:=make(chan struct{});go func(){defer syscall.CloseHandle(h);for{
  select{case <-done:return;default:}
  state,e:=syscall.WaitForSingleObject(h,250);if e!=nil||state==0{_ = os.WriteFile(stop,[]byte("patcher parent exited"),0644);return}
 }}();return func(){close(done)},nil
}
