export type Notification = {
  eventId:string; contextId:string; tenantId:string; resourceId:string;
  resourceRevision:number; occurredAt:string; type:string; correlationId:string;
  payload:Record<string,unknown>;
};

// A notification invalidates a snapshot; it never grants access or supplies video.
export function onEnvironmentChange(types:string[], refresh:()=>void) {
  const receive=(event:Event)=>{
    const value=(event as CustomEvent<Notification>).detail;
    if(types.includes(value.type)||["AuthorizationChanged","ResyncRequired","Connected"].includes(value.type))refresh();
  };
  window.addEventListener("vms:change",receive);
  return()=>window.removeEventListener("vms:change",receive);
}
