export type PTZResources={panTilt:boolean;zoom:boolean;home:boolean;revision:string};
export type PTZAction={pan?:number;tilt?:number;zoom?:number;home?:boolean};
type Command={id:string;expiresInMs:number};
type Transport=(method:"POST"|"PUT"|"DELETE",action?:PTZAction,id?:string)=>Promise<Command|void>;
type Gesture={ended:boolean;id?:string;timer?:ReturnType<typeof setTimeout>;stopping?:Promise<void>;generation:number};

// One instance belongs to one camera/source/tab. Late starts are followed by
// their own stop; stale renewals never create new movement or reclaim control.
export class PTZControlSession {
 private current:Gesture|null=null;
 private tail:Promise<void>=Promise.resolve();
 private disposed=false;
 private generation=0;
 constructor(private transport:Transport,private feedback:(message:string)=>void,private active:(value:boolean)=>void){}

 start(action:PTZAction,message:string){
  this.end();
  const gesture:Gesture={ended:false,generation:++this.generation};
  this.current=gesture;this.active(true);this.feedback("Enviando comando…");
  this.tail=this.tail.then(async()=>{
   if(gesture.ended||this.disposed)return;
   try{
    const command=await this.transport("POST",action);
    if(!command)throw new Error("O serviço não confirmou o comando PTZ.");
    gesture.id=command.id;
    if(gesture.ended||this.disposed){await this.stop(gesture);return;}
    this.feedback(message);this.schedule(gesture);
   }catch(error){
    if(this.current===gesture){this.current=null;gesture.ended=true;this.active(false);this.report(error);}
   }
  });
  return gesture.generation;
 }

 end(generation?:number){
  const gesture=this.current;if(!gesture)return;
  if(generation!==undefined&&gesture.generation!==generation)return;
  this.current=null;gesture.ended=true;clearTimeout(gesture.timer);
  if(!this.disposed)this.active(false);
  if(gesture.id)void this.stop(gesture);
 }

 private stop(gesture:Gesture):Promise<void>{
  return gesture.stopping??=this.transport("DELETE",undefined,gesture.id).then(()=>{
   if(!this.disposed&&this.generation===gesture.generation&&!this.current)this.feedback("Comando encerrado.");
  }).catch(error=>{if(!this.disposed&&this.generation===gesture.generation)this.report(error);});
 }

 private schedule(gesture:Gesture){
  gesture.timer=setTimeout(async()=>{
   if(gesture.ended||this.current!==gesture||this.disposed)return;
   try{await this.transport("PUT",undefined,gesture.id);if(!gesture.ended&&this.current===gesture)this.schedule(gesture);}
   catch(error){
    if(this.current===gesture){this.current=null;gesture.ended=true;this.active(false);await this.stop(gesture);this.report(error);}
   }
  },400);
 }
 private report(error:unknown){if(!this.disposed)this.feedback(error instanceof Error?error.message:"Não foi possível controlar a câmera.");}
 dispose(){this.disposed=true;this.end();}
}
