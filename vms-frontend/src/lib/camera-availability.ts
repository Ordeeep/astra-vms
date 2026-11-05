export type AvailabilityState="online"|"offline"|"unknown"|"suspended";
export type VideoObservation={profile:string;available:boolean;complete:boolean;code?:string;checkedAt:string;lastVideoAt?:string};
export type CameraAvailability={state:AvailabilityState;code?:string;checkedAt?:string;lastVideoAt?:string;validUntil?:string;profile?:string;warnings?:VideoObservation[]};
export const availabilityLabels:Record<AvailabilityState,string>={online:"Online",offline:"Offline",unknown:"Sem informação",suspended:"Monitoramento suspenso"};
export const availabilityReasons:Record<string,string>={
 awaiting_check:"Aguardando a primeira verificação.",
 confirming_failure:"Falha inicial. Aguardando confirmação na próxima verificação.",
 camera_inactive:"A câmera, o site ou o tenant está desativado.",
 stale_check:"A última verificação ficou desatualizada.",
 monitor_unavailable:"Não foi possível atualizar o acompanhamento.",
 worker_unavailable:"O serviço de vídeo está indisponível para verificar a câmera.",
 monitor_capacity:"A verificação não pôde ser concluída neste ciclo.",
 check_incomplete:"Não foi possível concluir a verificação de todos os perfis.",
 profiles_unavailable:"Os perfis de vídeo ainda não estão disponíveis.",
 vault_unavailable:"Não foi possível acessar as credenciais protegidas.",
 invalid_configuration:"Não foi possível utilizar a configuração de vídeo atual.",
 authentication_failed:"A câmera recusou a autenticação.",
 connection_failed:"Não foi possível estabelecer conexão com a câmera.",
 description_failed:"Não foi possível consultar o vídeo da câmera.",
 setup_failed:"Não foi possível preparar a recepção do vídeo.",
 play_failed:"A câmera não iniciou a transmissão de vídeo.",
 video_timeout:"Não chegou vídeo dentro do tempo de verificação.",
 video_unavailable:"Não foi possível receber vídeo da câmera.",
 unsupported_video:"O formato de vídeo não pôde ser verificado.",
};
export function cameraAvailability(camera:{effectiveActive:boolean;availability?:CameraAvailability},now=Date.now()):CameraAvailability{
 if(!camera.effectiveActive)return {state:"suspended",code:"camera_inactive"};
 const status=camera.availability;
 if(!status||!Object.hasOwn(availabilityLabels,status.state))return {state:"unknown",code:"awaiting_check"};
 if((status.state==="online"||status.state==="offline")&&(!status.validUntil||!Number.isFinite(Date.parse(status.validUntil))||now>=Date.parse(status.validUntil)))return {...status,state:"unknown",code:"stale_check",profile:undefined,warnings:[]};
 return status;
}
