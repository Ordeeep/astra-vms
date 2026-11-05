export type Operator = {id: string; username: string; name: string; email: string; globalAdmin: boolean; active: boolean; revision: number};
export type Tenant = {id: string; name: string; active: boolean; revision: number};
export type Identity = {user: Operator; tenants: Tenant[]; csrfToken: string};
export class ApiError extends Error {
  constructor(public status: number, public code: string, public details:Record<string,unknown>={}) {
    super(messages[code] ?? `Não foi possível concluir a operação (${status}).`);
  }
}
const messages: Record<string,string> = {
  ptz_unsupported: "Esta câmera não oferece este recurso de PTZ.",
  invalid_ptz_command: "O comando PTZ é inválido.",
  ptz_superseded: "Outro comando assumiu o PTZ. Solte o controle e pressione novamente para assumir.",
  ptz_expired: "O comando PTZ expirou. Solte e pressione novamente.",
  ptz_source_changed: "A conexão da câmera mudou. Atualize os recursos PTZ antes de controlar.",
  ptz_camera_inactive: "Esta câmera está desativada para operação.",
  ptz_device_busy: "A câmera está ocupada. Tente novamente.",
  ptz_unavailable: "O controle PTZ está indisponível. Tente novamente.",
  ptz_capacity: "Há muitos controles PTZ em andamento. Aguarde e tente novamente.",
  camera_api_unavailable: "Não foi possível comunicar com a câmera.",
  camera_api_timeout: "A câmera não respondeu ao comando a tempo.",
  camera_auth_failed: "A câmera recusou as credenciais ou a permissão de controle.",
  camera_api_response_invalid: "A câmera retornou uma resposta inválida.",
  camera_api_unsupported: "A câmera não oferece esta operação de API.",
  invalid_mosaic: "Informe um nome válido e um mosaico de até 16 posições.",
  mosaic_not_found: "Esta visualização não está mais disponível.",
  camera_not_found: "Uma das câmeras não está mais disponível neste ambiente. Atualize o mosaico e tente novamente.",
  invalid_connection: "Informe endereço, portas e credenciais válidos para a câmera.",
  camera_discovery_required: "Os perfis ainda não foram identificados. Conecte a câmera e consulte seus perfis para testar o vídeo.",
  invalid_profiles: "Informe o perfil principal e perfis de vídeo válidos.",
  unsupported_connection_type: "Este tipo de conexão ainda não está disponível.",
  use_source_replacement: "Edite o tipo de conexão e os perfis em Informações.",
  profile_unavailable: "O perfil selecionado não está disponível nesta fonte de vídeo.",
  vault_unavailable: "Não foi possível acessar as credenciais protegidas da câmera. Tente novamente.",
  media_unavailable: "Não foi possível preparar o vídeo. Verifique a câmera ou tente novamente.",
  invalid_rtsp_url: "Informe um endereço RTSP ou RTSPS válido.",
  source_probe_failed: "Não foi possível acessar o vídeo. Verifique o endereço RTSP, as credenciais e a conexão com a câmera.",
  probe_capacity: "Há muitos testes em andamento. Aguarde e tente novamente.",
  incompatible_api:"A interface e o serviço da instalação usam versões incompatíveis. Atualize os componentes pelo mesmo pacote.",
  invalid_credentials: "Usuário, e-mail ou senha inválidos.",
  current_password_required: "Confirme sua senha atual para alterar o e-mail.",
  current_password_incorrect: "A senha atual está incorreta.",
  email_unavailable: "Este e-mail não está disponível. Use outro endereço.",
  operator_conflict: "Este usuário já está em uso ou corresponde ao e-mail de outra conta.",
  invalid_profile_name: "Informe um nome válido com até 128 caracteres.",
  invalid_email: "Informe um endereço de e-mail válido com até 254 caracteres.",
  invalid_password: "Informe uma senha com até 1.024 caracteres.",
  authentication_busy: "O serviço está ocupado. Aguarde um momento e tente novamente.",
  authentication_required: "Entre novamente para continuar.",
  csrf_required: "Sua sessão mudou. Atualize a página.",
  tenant_not_found: "Este ambiente não está disponível para sua conta.",
  context_not_found: "O contexto deste ambiente não está mais disponível.",
  database_unavailable: "O serviço de dados está indisponível.",
  too_many_attempts: "Muitas tentativas. Aguarde antes de tentar novamente.",
  forbidden: "Sua conta não tem permissão para esta operação.",
  quota_reduction_required:"Para reduzir a cota abaixo do uso atual, utilize Redução de cota. O orçamento de exportações só pode ser reduzido após a limpeza desses arquivos.",
  quota_job_active:"Já há uma redução de cota em andamento neste ambiente.",
  invalid_reduction:"Escolha uma cota menor que o limite atual.",
  quota_preview_expired:"Prepare novamente a redução antes de confirmar.",
  export_finished:"Esta exportação já terminou. Seus arquivos foram preservados.",
  revision_conflict: "Este cadastro foi alterado. Atualize antes de salvar.",
  export_budget_unavailable: "O orçamento de exportações não comporta este intervalo. Reduza o período ou aguarde a limpeza dos arquivos anteriores.",
  export_has_no_images: "Não há imagens gravadas nesse intervalo.",
  export_preview_expired: "Esta prévia expirou. Prepare outra antes de gerar.",
  source_changed_repreview: "Parte do vídeo ficou indisponível. Prepare uma nova prévia.",
  invalid_export_window: "Selecione um intervalo válido de até 24 horas.",
  archive_requires_review: "Os horários ou parâmetros das gravações não permitem gerar este intervalo. Consulte o diagnóstico.",
};
let csrf = "";
let currentContext = "";
export function setCSRF(value: string) { csrf = value; }
export function setContext(value: string) { currentContext = value; }
export async function api<T>(path: string, method = "GET", data?: unknown, scoped = false): Promise<T> {
  return requestAPI<T>(path,method,data,{scoped,context:currentContext,token:csrf});
}
// Freeze the tab context for cleanup after a tenant switch. A late DELETE must
// never borrow the NEW tenant context to stop an OLD camera command.
// Return even stale responses to this owner, so it can stop a late POST. The
// component/session must discard stale display data and dispose its commands.
export function captureScopedAPI() {
  const context=currentContext,token=csrf;
  return <T>(path:string,method="GET",data?:unknown)=>requestAPI<T>(path,method,data,{scoped:true,context,token,keepalive:method==="DELETE",bounded:true,acceptStaleContext:true});
}
type RequestScope={scoped:boolean;context:string;token:string;keepalive?:boolean;bounded?:boolean;acceptStaleContext?:boolean};
async function requestAPI<T>(path:string,method:string,data:unknown,{scoped,context,token,keepalive=false,bounded=false,acceptStaleContext=false}:RequestScope):Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    method, credentials: "same-origin", cache: "no-store", keepalive,
    ...(bounded&&!keepalive?{signal:AbortSignal.timeout(10000)}:{}),
    headers: {
      "Content-Type": "application/json",
      ...(token ? {"X-CSRF-Token": token} : {}),
      ...(scoped ? {"X-VMS-Context": context} : {}),
    },
    ...(data === undefined ? {} : {body: JSON.stringify(data)}),
  });
  const result = res.status === 204 ? undefined : await res.json().catch(() => ({code:"request_failed"}));
  if(res.headers.get("X-VMS-Contract")!=="1")throw new ApiError(409,"incompatible_api");
  if (scoped && !acceptStaleContext && context !== currentContext) throw new DOMException("Contexto alterado", "AbortError");
  if (!res.ok) {
    throw new ApiError(res.status, result.code,result);
  }
  return result as T;
}
export function mediaContext(){const context=currentContext;return {headers:{"X-VMS-Context":context},valid:()=>context===currentContext};}
export async function signalMedia(path:string,sdp:string,signal:AbortSignal):Promise<string>{
  const context=currentContext;
  const response=await fetch(path,{method:"POST",credentials:"same-origin",cache:"no-store",signal,headers:{"Content-Type":"application/sdp","X-CSRF-Token":csrf,"X-VMS-Context":context},body:sdp});
  const body=await response.text();
  if(context!==currentContext)throw new DOMException("Contexto alterado","AbortError");
  if(!response.ok)throw new ApiError(response.status,"media_unavailable");
  return body;
}
