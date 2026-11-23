"use client";

import {useEffect, useRef, useState, type FormEvent, type ReactNode} from "react";
import {AlertDialog, Dialog} from "radix-ui";
import {X} from "lucide-react";
import {Button} from "@/components/ui/button";

type FocusReturn = {
  returnFocus?: HTMLElement | null;
  fallbackFocus?: () => HTMLElement | null;
};

function useReturnFocus({returnFocus, fallbackFocus}: FocusReturn) {
  const origin = useRef(returnFocus ?? (typeof document === "undefined" ? null : document.activeElement as HTMLElement | null));

  return (event: Event) => {
    const target = origin.current?.isConnected && !origin.current.matches("html, body, :disabled") ? origin.current : fallbackFocus?.();
    if (target) {
      event.preventDefault();
      target.focus();
    }
  };
}

function useDialogClose(onClose: () => void) {
  const [open, setOpen] = useState(true);
  const closing = useRef(false);

  return {
    open,
    close() {
      closing.current = true;
      setOpen(false);
    },
    afterClose() {
      if (!closing.current) return;
      closing.current = false;
      onClose();
    },
  };
}

type ConfirmationProps = FocusReturn & {
  title: string;
  description: ReactNode;
  confirmLabel: string;
  onCancel: () => void;
  onConfirm: () => void;
  busy?: boolean;
  children?: ReactNode;
};

export function ConfirmationDialog({title, description, confirmLabel, onCancel, onConfirm, busy, children, ...focus}: ConfirmationProps) {
  const restoreFocus = useReturnFocus(focus);
  const {open, close, afterClose} = useDialogClose(onCancel);

  return <AlertDialog.Root open={open}>
    <AlertDialog.Portal>
      <AlertDialog.Overlay className="camera-dialog-overlay" data-slot="dialog-overlay" />
      <AlertDialog.Content className="camera-dialog confirmation-dialog" data-slot="alert-dialog-content"
        onEscapeKeyDown={event => event.preventDefault()} onCloseAutoFocus={event => {restoreFocus(event);afterClose();}}>
        <div className="dialog-header"><AlertDialog.Title asChild><h2>{title}</h2></AlertDialog.Title></div>
        <div className="dialog-body"><AlertDialog.Description asChild><p>{description}</p></AlertDialog.Description>{children}</div>
        <div className="dialog-footer">
          <AlertDialog.Cancel asChild><Button type="button" variant="outline" disabled={busy} onClick={close}>Cancelar</Button></AlertDialog.Cancel>
          <AlertDialog.Action asChild><Button type="button" variant="destructive" disabled={busy} onClick={onConfirm}>{confirmLabel}</Button></AlertDialog.Action>
        </div>
      </AlertDialog.Content>
    </AlertDialog.Portal>
  </AlertDialog.Root>;
}

type EditorDialogProps = FocusReturn & {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  wide?: boolean;
  busy?: boolean;
  className?: string;
};

function DialogFrame({title, onClose, children, wide, busy, className = "", returnFocus, fallbackFocus, open, afterClose}: Omit<EditorDialogProps, "footer"> & {
  open: boolean;
  afterClose: () => void;
}) {
  const restoreFocus = useReturnFocus({returnFocus, fallbackFocus});
  function requestClose() {
    // A confirmation owns dismissal while it is displayed above an editor.
    if (open && !busy && !document.querySelector('[data-slot="alert-dialog-content"]')) onClose();
  }

  return <Dialog.Root open={open} onOpenChange={next => {if (!next) requestClose();}}>
    <Dialog.Portal>
      <Dialog.Overlay className="camera-dialog-overlay" data-slot="dialog-overlay"
        onClick={event => {if (event.target === event.currentTarget) requestClose();}} />
      <Dialog.Content className={`camera-dialog${wide ? " editor-dialog-wide" : ""} ${className}`} data-slot="dialog-content"
        aria-describedby={undefined} onCloseAutoFocus={event => {restoreFocus(event);afterClose();}} onPointerDownOutside={event => event.preventDefault()}>
        <div className="dialog-header" data-slot="dialog-header">
          <Dialog.Title asChild><h2>{title}</h2></Dialog.Title>
          <Dialog.Close asChild><Button type="button" variant="ghost" size="icon" className="dialog-close" aria-label="Fechar" disabled={busy}><X aria-hidden="true" /></Button></Dialog.Close>
        </div>
        {children}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

export function EditorDialog({children, footer, onClose, ...props}: EditorDialogProps) {
  const {open, close, afterClose} = useDialogClose(onClose);

  return <DialogFrame {...props} open={open} onClose={close} afterClose={afterClose}>
    <div className="dialog-body">{children}</div>
    {footer && <div className="dialog-footer" data-slot="dialog-footer">{footer}</div>}
  </DialogFrame>;
}

type FormDialogProps = Omit<EditorDialogProps, "footer"> & {
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  saveLabel?: string | null;
  busyLabel?: string;
  noValidate?: boolean;
  saveDisabled?: boolean;
  footerNote?: ReactNode;
  footerActions?: ReactNode;
  hasChanges?: boolean;
};

export function FormDialog({onSubmit, children, busy = false, saveLabel = "Salvar", busyLabel = "Salvando…", onClose, noValidate = false,
  saveDisabled = false, footerNote, footerActions, hasChanges, ...props}: FormDialogProps) {
  const form = useRef<HTMLFormElement>(null);
  const wasBusy = useRef(false);
  const edited = useRef(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const {open, close, afterClose} = useDialogClose(onClose);

  function requestClose() {
    if (hasChanges ?? edited.current) setConfirmDiscard(true);
    else close();
  }

  useEffect(() => {
    // The submit button can lose focus while it is disabled during the request.
    if (wasBusy.current && !busy && form.current && !document.querySelector('[role="alertdialog"]') &&
      !form.current.closest('[role="dialog"]')?.contains(document.activeElement)) {
      form.current.querySelector<HTMLButtonElement>('button[type="submit"]')?.focus();
    }
    wasBusy.current = busy;
  }, [busy]);

  return <>
    <DialogFrame {...props} open={open} onClose={requestClose} afterClose={afterClose} busy={busy}>
      <form ref={form} onSubmit={onSubmit} aria-busy={busy} noValidate={noValidate}
        onChangeCapture={event => {if (!(event.target as HTMLElement).closest("[data-form-view-control]")) edited.current = true;}}
        onClickCapture={event => {if ((event.target as HTMLElement).closest("[data-form-change]")) edited.current = true;}}>
        <div className="dialog-form-body"><fieldset className="dialog-form-fields" disabled={busy}>{children}</fieldset></div>
        <div className="form-actions dialog-footer" data-slot="dialog-footer">
          {footerNote && <div className="dialog-footer-note">{footerNote}</div>}
          {footerActions && <div className="dialog-footer-secondary">{footerActions}</div>}
          <Button type="button" variant="outline" onClick={requestClose} disabled={busy}>Cancelar</Button>
          {saveLabel !== null && <Button type="submit" disabled={busy || saveDisabled}>{busy ? busyLabel : saveLabel}</Button>}
        </div>
      </form>
    </DialogFrame>
    {confirmDiscard && <ConfirmationDialog title="Descartar alterações?" description="As alterações que você fez neste formulário serão perdidas."
      confirmLabel="Descartar alterações" onCancel={() => setConfirmDiscard(false)} onConfirm={() => {setConfirmDiscard(false);close();}} />}
  </>;
}
