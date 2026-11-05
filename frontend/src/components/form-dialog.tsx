"use client";

import {useEffect, useRef, type FormEvent, type ReactNode} from "react";
import {Dialog} from "radix-ui";
import {Button} from "@/components/ui/button";

type EditorDialogProps = {
  title: string;
  onClose: () => void;
  children: ReactNode;
  returnFocus?: HTMLElement | null;
  fallbackFocus?: () => HTMLElement | null;
  wide?: boolean;
  busy?: boolean;
  showClose?: boolean;
  className?: string;
};

/** Mount only while open; the trigger stays in the page to restore keyboard focus. */
export function EditorDialog({title,onClose,children,returnFocus,fallbackFocus,wide=false,busy=false,showClose=true,className=""}: EditorDialogProps) {
  const origin = useRef(returnFocus ?? (typeof document !== "undefined" ? document.activeElement as HTMLElement : null));
  return <Dialog.Root open onOpenChange={open=>{if(!open&&!busy)onClose();}}>
    <Dialog.Portal>
      <Dialog.Overlay className="camera-dialog-overlay"/>
      <Dialog.Content className={`camera-dialog editor-panel${wide?" editor-dialog-wide":""} ${className}`} aria-describedby={undefined}
        onCloseAutoFocus={event=>{const target=origin.current?.isConnected?origin.current:fallbackFocus?.();if(target){event.preventDefault();target.focus();}}}
        onPointerDownOutside={event=>event.preventDefault()}>
        <div className="section-title">
          <Dialog.Title asChild><h2>{title}</h2></Dialog.Title>
          {showClose&&<Button type="button" variant="ghost" onClick={onClose} disabled={busy}>Fechar</Button>}
        </div>
        {children}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

type FormDialogProps = EditorDialogProps & {
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  saveLabel?: string | null;
  noValidate?: boolean;
  saveDisabled?: boolean;
  footerNote?: ReactNode;
};

export function FormDialog({onSubmit,children,busy=false,saveLabel="Salvar",onClose,noValidate=false,saveDisabled=false,footerNote,...props}: FormDialogProps) {
  const form = useRef<HTMLFormElement>(null);
  const wasBusy = useRef(false);
  useEffect(()=>{
    // A disabled submit button can release focus while the request is pending.
    if(wasBusy.current&&!busy&&form.current&&!form.current.closest('[role="dialog"]')?.contains(document.activeElement)) {
      form.current.querySelector<HTMLButtonElement>('button[type="submit"]')?.focus();
    }
    wasBusy.current=busy;
  },[busy]);
  return <EditorDialog {...props} onClose={onClose} busy={busy}>
    <form ref={form} onSubmit={onSubmit} aria-busy={busy} noValidate={noValidate}>
      <div className="dialog-form-body"><fieldset className="dialog-form-fields" disabled={busy}>{children}</fieldset></div>
      <div className="form-actions">
        {saveLabel!==null&&<Button type="submit" disabled={busy || saveDisabled}>{busy?"Salvando…":saveLabel}</Button>}
        <Button type="button" variant="ghost" onClick={onClose} disabled={busy}>Cancelar</Button>
        {footerNote}
      </div>
    </form>
  </EditorDialog>;
}
