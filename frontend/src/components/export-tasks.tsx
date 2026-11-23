"use client";

import {useEffect,useState} from "react";
import {Download} from "lucide-react";
import {ConfirmationDialog,EditorDialog} from "@/components/form-dialog";
import {Alert,AlertDescription} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {Progress} from "@/components/ui/progress";
import {Table,TableBody,TableCell,TableHead,TableHeader,TableRow} from "@/components/ui/table";
import {Skeleton} from "@/components/ui/skeleton";
import {Tooltip,TooltipContent,TooltipProvider,TooltipTrigger} from "@/components/ui/tooltip";
import {api,ApiError,mediaContext} from "@/lib/api";

type Task = {
  id: string;
  createdAt: string;
  cameraName: string;
  start: string;
  end: string;
  author: string;
  status: "queued" | "running" | "completed" | "failed" | "cancelled" | "expired";
  code?: string;
  frames: number;
  total: number;
  hasGaps: boolean;
  canDownload: boolean;
  downloadExpired: boolean;
};
type TaskPage = {items: Task[]; nextCursor: string; hasActive: boolean};

const dateTime = new Intl.DateTimeFormat("pt-BR",{dateStyle:"short",timeStyle:"medium"});
const statusLabel: Record<Task["status"],string> = {
  queued:"Na fila",running:"Em andamento",completed:"Concluída",failed:"Falhou",cancelled:"Cancelada",expired:"Expirada",
};
const failureReasons = new Map([
  ["export_budget_unavailable", "O espaço disponível para exportações era insuficiente para este vídeo."],
  ["source_changed_repreview", "Parte da gravação ficou indisponível após a solicitação."],
  ["source_or_encoding_failed", "O serviço de mídia não conseguiu ler a gravação ou codificar o vídeo."],
  ["export_failed", "O serviço de mídia não conseguiu salvar ou finalizar os arquivos da exportação."],
  ["worker_job_lost", "O serviço de mídia perdeu o processamento desta tarefa."],
  ["export_plan_rejected", "O serviço de mídia recusou os parâmetros desta exportação."],
  ["export_authority_expired", "A autorização de processamento expirou antes da conclusão."],
  ["export_cancelled", "O processamento da exportação foi interrompido."],
  ["requestor_access_revoked_or_expired", "O acesso à gravação foi revogado ou o prazo da tarefa expirou."],
]);

function taskActive(task: Task) {
  return task.status === "queued" || task.status === "running";
}

function TaskDate({value}:{value:string}) {
  if (!Number.isFinite(Date.parse(value))) return <>—</>;
  return <time dateTime={value}>{dateTime.format(new Date(value))}</time>;
}

function TaskProgress({task}:{task:Task}) {
  if (task.status === "failed") return <Tooltip>
    <TooltipTrigger asChild>
      <span tabIndex={0} className="inline-block cursor-help rounded-sm underline decoration-dotted underline-offset-2">{statusLabel.failed}</span>
    </TooltipTrigger>
    <TooltipContent className="z-[60]! max-w-[min(20rem,calc(100vw-24px))]! whitespace-normal" sideOffset={6} collisionPadding={12}>
      {failureReasons.get(task.code ?? "") ?? "O servidor não registrou um motivo específico para esta falha."}
    </TooltipContent>
  </Tooltip>;

  const percent = task.total > 0 ? Math.max(0,Math.min(99,Math.floor(100*task.frames/task.total))) : 0;
  return <div className="export-task-progress">
    <span>{statusLabel[task.status]}{task.status === "running" && ` · ${percent}%`}{task.status === "completed" && " · 100%"}</span>
    {task.status === "running" && <Progress value={percent} aria-label={`Progresso de ${task.cameraName}`}/>}
    {task.hasGaps && task.status === "completed" && <small>Trechos sem gravação aparecem em preto.</small>}
  </div>;
}

export function ExportTasksDialog({tenantId,trigger,onClose}:{tenantId:string;trigger:HTMLElement;onClose:()=>void}) {
  const [pages,setPages] = useState([""]);
  const cursor = pages[pages.length-1];
  const [loaded,setLoaded] = useState<{cursor:string;page:TaskPage}>();
  const page = loaded?.cursor === cursor ? loaded.page : undefined;
  const [refresh,setRefresh] = useState(0);
  const [loadError,setLoadError] = useState("");
  const [selected,setSelected] = useState("");
  const [busy,setBusy] = useState(false);
  const [confirmCancelAll,setConfirmCancelAll] = useState(false);
  const [error,setError] = useState("");
  const [notice,setNotice] = useState("");
  const base = `/tenants/${encodeURIComponent(tenantId)}`;
  const selectedTask = page?.items.find(task=>task.id === selected && taskActive(task));

  useEffect(()=>{
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const path = `${base}/export-tasks${cursor ? `?${new URLSearchParams({cursor})}` : ""}`;
    async function load() {
      let delay = 5000;
      try {
        const signal = AbortSignal.any([controller.signal,AbortSignal.timeout(10000)]);
        const result = await api<TaskPage>(path,"GET",undefined,true,signal);
        if (controller.signal.aborted) return;
        setLoaded({cursor,page:result});
        setLoadError("");
        if (result.hasActive) delay = 1000;
      } catch (error) {
        if (controller.signal.aborted || (error instanceof Error && error.name === "AbortError")) return;
        setLoadError("Não foi possível atualizar as tarefas.");
      }
      timer = setTimeout(()=>void load(),delay);
    }
    void load();
    return ()=>{controller.abort();clearTimeout(timer);};
  },[base,cursor,refresh]);

  async function cancel(job?: Task) {
    if (busy) return;
    const context = mediaContext();
    setBusy(true);
    setNotice("");
    setError("");
    try {
      const signal = AbortSignal.timeout(10000);
      if (job) {
        await api(`${base}/exports/${encodeURIComponent(job.id)}/cancel`,"POST",undefined,true,signal);
        setNotice("Tarefa cancelada.");
      } else {
        const {cancelled} = await api<{cancelled:number}>(`${base}/exports/cancel`,"POST",undefined,true,signal);
        setConfirmCancelAll(false);
        setNotice(cancelled === 1 ? "1 tarefa cancelada." : cancelled > 1 ? `${cancelled} tarefas canceladas.` : "Nenhuma tarefa aguardava cancelamento.");
      }
      setSelected("");
    } catch (error) {
      if (!context.valid()) return;
      setError(error instanceof ApiError && error.code === "export_finished" ? "Esta tarefa já terminou." : "Não foi possível cancelar. Tente novamente.");
    } finally {
      if (context.valid()) {
        setBusy(false);
        setRefresh(value=>value+1);
      }
    }
  }

  function changePage(next: string[]) {
    setLoadError("");
    setSelected("");
    setPages(next);
  }

  const footer = <div className="export-task-footer">
    <div className="flex flex-wrap gap-2">
      <Button type="button" variant="outline" disabled={busy || !selectedTask} onClick={()=>{if (selectedTask) void cancel(selectedTask);}}>Cancelar selecionado</Button>
      <Button type="button" variant="destructive" disabled={busy || !page?.hasActive} onClick={()=>{setError("");setConfirmCancelAll(true);}}>Cancelar todos</Button>
    </div>
    {(pages.length > 1 || page?.nextCursor) && <nav aria-label="Páginas das tarefas" className="flex gap-2">
      <Button type="button" variant="outline" disabled={busy || pages.length === 1} onClick={()=>changePage(pages.slice(0,-1))}>Anterior</Button>
      <Button type="button" variant="outline" disabled={busy || !page?.nextCursor} onClick={()=>{if (page?.nextCursor) changePage([...pages,page.nextCursor]);}}>Próxima</Button>
    </nav>}
  </div>;

  return <><EditorDialog title="Janela de tarefas" wide className="export-tasks-dialog" busy={busy} onClose={onClose} returnFocus={trigger} footer={footer}>
    {loadError && <Alert variant="destructive" role="alert" className="alert">
      <AlertDescription>{loadError} <Button type="button" size="sm" variant="outline" onClick={()=>setRefresh(value=>value+1)}>Atualizar</Button></AlertDescription>
    </Alert>}
    {!page && !loadError && <div role="status"><span className="sr-only">Carregando tarefas…</span><Skeleton className="h-36 w-full"/></div>}
    {page && page.items.length === 0 && <p>Nenhuma tarefa de exportação.</p>}
    {page && page.items.length > 0 && <TooltipProvider delayDuration={200}>
      <Table role="table" aria-label="Tarefas de exportação" containerProps={{className:"export-task-list",role:"region","aria-label":"Lista de tarefas",tabIndex:0}}>
        <TableHeader role="rowgroup"><TableRow role="row">
          <TableHead scope="col"><span className="sr-only">Selecionar tarefa</span></TableHead>
          <TableHead scope="col">Tarefa criada</TableHead>
          <TableHead scope="col">Fonte</TableHead>
          <TableHead scope="col">Hora de início</TableHead>
          <TableHead scope="col">Hora final</TableHead>
          <TableHead scope="col">Progresso</TableHead>
          <TableHead scope="col">Autor</TableHead>
          <TableHead scope="col">Download</TableHead>
        </TableRow></TableHeader>
        <TableBody role="rowgroup">{page.items.map(task=><TableRow role="row" key={task.id} data-state={selectedTask?.id === task.id ? "selected" : undefined}>
          <TableCell role="cell">{taskActive(task) && <input type="radio" name="export-task" checked={selectedTask?.id === task.id} disabled={busy}
            aria-label={`Selecionar tarefa de ${task.cameraName} criada em ${dateTime.format(new Date(task.createdAt))}`} onChange={()=>setSelected(task.id)}/>}</TableCell>
          <TableCell role="cell" data-label="Tarefa criada" className="tabular-nums"><TaskDate value={task.createdAt}/></TableCell>
          <TableCell role="cell" data-label="Fonte" className="export-task-name">{task.cameraName || "—"}</TableCell>
          <TableCell role="cell" data-label="Hora de início" className="tabular-nums"><TaskDate value={task.start}/></TableCell>
          <TableCell role="cell" data-label="Hora final" className="tabular-nums"><TaskDate value={task.end}/></TableCell>
          <TableCell role="cell" data-label="Progresso"><TaskProgress task={task}/></TableCell>
          <TableCell role="cell" data-label="Autor" className="export-task-name">{task.author}</TableCell>
          <TableCell role="cell" data-label="Download">{task.canDownload ? <Button asChild variant="outline" size="sm">
            <a href={`/api/v1${base}/exports/${encodeURIComponent(task.id)}/content?${new URLSearchParams({context:mediaContext().headers["X-VMS-Context"]})}`} download={`${task.id}.mp4`}>
              <Download aria-hidden="true"/>Baixar vídeo
            </a>
          </Button> : task.downloadExpired ? "Expirado" : task.status === "completed" ? "Indisponível" : "—"}</TableCell>
        </TableRow>)}</TableBody>
      </Table>
    </TooltipProvider>}
    {error && !confirmCancelAll && <Alert variant="destructive" role="alert" className="alert"><AlertDescription>{error}</AlertDescription></Alert>}
    {notice && <p role="status" className="export-task-feedback">{notice}</p>}
  </EditorDialog>
  {confirmCancelAll && <ConfirmationDialog title="Cancelar todas as tarefas?" busy={busy}
    description="Todas as suas tarefas na fila ou em processamento neste ambiente serão canceladas."
    confirmLabel={busy ? "Cancelando…" : "Cancelar todas as tarefas"} onConfirm={()=>void cancel()}
    fallbackFocus={()=>document.querySelector('.export-tasks-dialog .dialog-close')}
    onCancel={()=>{setError("");setConfirmCancelAll(false);}}>
    {error && <Alert variant="destructive" role="alert"><AlertDescription>{error}</AlertDescription></Alert>}
  </ConfirmationDialog>}</>;
}
