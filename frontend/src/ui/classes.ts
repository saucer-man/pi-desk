/** Shared Tailwind atoms for recurring desktop-control shapes. */
export const ui = {
  root:
    "min-w-0 font-body text-[var(--text)]",
  settingsControls:
    "[&_.text-button]:!h-7 [&_.text-button]:!min-h-7 [&_.text-button]:!gap-1.5 [&_.text-button]:!rounded-md [&_.text-button]:!px-2.5 [&_.text-button]:!text-xs [&_.text-button]:!leading-none [&_.text-button]:!shadow-none [&_.text-button_svg]:!size-[13px] [&_.icon-button]:!size-7 [&_.icon-button]:!min-h-7 [&_.icon-button]:!basis-7 [&_.icon-button]:!rounded-md [&_.icon-button]:!shadow-none [&_.icon-button_svg]:!size-[13px] [&_select]:!h-7 [&_select]:!min-h-7 [&_select]:!rounded-md [&_select]:!px-2.5 [&_select]:!text-xs [&_input:not([type=checkbox]):not([type=radio])]:!h-7 [&_input:not([type=checkbox]):not([type=radio])]:!min-h-7 [&_input:not([type=checkbox]):not([type=radio])]:!rounded-md [&_input:not([type=checkbox]):not([type=radio])]:!px-2.5 [&_input:not([type=checkbox]):not([type=radio])]:!text-xs [&_input[type=checkbox]]:!size-3.5 [&_input[type=checkbox]]:!min-h-0 [&_input[type=checkbox]]:!basis-3.5 [&_input[type=checkbox]]:!p-0 [&_input[type=radio]]:!size-3.5 [&_input[type=radio]]:!min-h-0 [&_input[type=radio]]:!basis-3.5 [&_input[type=radio]]:!p-0 [&_textarea]:!rounded-md [&_textarea]:!px-2.5 [&_textarea]:!py-2 [&_textarea]:!text-xs [&_.mcp-checkbox]:!h-7 [&_.mcp-checkbox]:!rounded-md [&_.mcp-checkbox]:!px-2.5 [&_.mcp-checkbox]:!text-xs [&_.resource-search]:!h-7 [&_.resource-search]:!rounded-md [&_.resource-search]:!px-2.5 [&_.resource-search_input]:!text-xs [&_.resource-filters_button]:!h-7 [&_.resource-filters_button]:!min-h-7 [&_.resource-filters_button]:!px-2.5 [&_.resource-filters_button]:!text-xs [&_.statistics-scope_button]:!h-7 [&_.statistics-scope_button]:!min-h-7 [&_.statistics-scope_button]:!px-2.5 [&_.statistics-scope_button]:!text-xs [&_.model-field-heading_button]:!h-7 [&_.model-field-heading_button]:!min-h-7 [&_.model-field-heading_button]:!rounded-md [&_.model-field-heading_button]:!px-2.5 [&_.model-field-heading_button]:!text-xs [&_.model-header-add]:!h-7 [&_.model-header-add]:!min-h-7 [&_.model-header-add]:!rounded-md [&_.model-header-add]:!px-2.5 [&_.model-header-add]:!text-xs",
  button:
    "inline-flex min-h-9 shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-[var(--border-strong)] bg-[var(--bg-raised)] px-3 text-sm font-medium text-[var(--text-secondary)] shadow-sm transition-[background-color,color,transform] duration-150 ease-out hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:translate-y-px active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50 pointer-coarse:min-h-11",
  buttonPrimary:
    "inline-flex min-h-9 shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-[var(--text)] bg-[var(--text)] px-3 text-sm font-semibold text-[var(--text-inverse)] shadow-sm transition-[opacity,transform] duration-150 ease-out hover:opacity-90 active:translate-y-px focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-40 pointer-coarse:min-h-11",
  buttonDanger:
    "inline-flex min-h-9 shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-[var(--red)] bg-[var(--bg-raised)] px-3 text-sm font-semibold text-[var(--red)] shadow-sm transition-[background-color,color,transform] duration-150 ease-out hover:bg-[var(--red)] hover:text-[var(--text-inverse)] active:translate-y-px focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--red)] disabled:cursor-not-allowed disabled:opacity-40 pointer-coarse:min-h-11",
  iconButton:
    "relative inline-grid size-8 shrink-0 place-items-center rounded-lg border border-transparent bg-transparent p-0 text-[var(--text-muted)] transition-[background-color,color,transform] duration-150 ease-out hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:translate-y-px active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-40 pointer-coarse:size-11",
  dialogBackdrop:
    "fixed inset-0 z-[400] grid place-items-center overflow-x-clip bg-[var(--overlay)] p-4 backdrop-blur-[2px] max-[520px]:place-items-stretch max-[520px]:p-0",
  dialog:
    "grid max-h-[min(88dvh,760px)] w-[min(640px,calc(100%_-_32px))] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-panel)] text-[var(--text)] shadow-2xl outline-none max-[520px]:h-dvh max-[520px]:max-h-dvh max-[520px]:w-full max-[520px]:rounded-none max-[520px]:border-0",
  dialogLarge:
    "w-[min(760px,calc(100%_-_32px))]! max-[520px]:w-full!",
  dialogWide:
    "w-[min(1040px,calc(100%_-_32px))]! max-[520px]:w-full!",
  dialogImage:
    "w-[min(960px,calc(100%_-_32px))]! max-[520px]:w-full!",
  dialogHeader:
    "flex min-h-14 items-center justify-between gap-4 border-b border-[var(--border)] px-5 py-3 [&_h2]:min-w-0 [&_h2]:font-display [&_h2]:text-base [&_h2]:font-semibold [&_h2]:tracking-[-0.02em] [&_h2]:text-[var(--text)]",
  dialogBody:
    "min-h-0 overflow-y-auto px-5 py-5 text-sm leading-relaxed text-[var(--text-secondary)] max-[520px]:px-4",
  dialogFooter:
    "flex min-h-16 items-center justify-end gap-2 border-t border-[var(--border)] px-5 py-3 max-[520px]:px-4",
  panel:
    "min-h-0 min-w-0 overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-raised)] text-[var(--text)] shadow-sm",
  panelHeader:
    "flex min-h-12 min-w-0 items-center justify-between gap-3 border-b border-[var(--border)] px-4 py-2",
  toolbar:
    "flex min-h-11 min-w-0 items-center justify-between gap-2 border-b border-[var(--border)] px-3",
  settingsContent:
    "min-h-0 min-w-0 overflow-y-auto bg-[var(--bg-panel)] p-5 text-[var(--text)] max-[520px]:p-4",
  settingsHeader:
    "mb-5 flex min-h-11 min-w-0 items-start justify-between gap-4 border-b border-[var(--border)] pb-4 [&_h2]:font-display [&_h2]:text-lg [&_h2]:font-semibold [&_h2]:tracking-[-0.025em] [&_p]:mt-1 [&_p]:max-w-[65ch] [&_p]:text-sm [&_p]:leading-relaxed [&_p]:text-[var(--text-muted)]",
  settingsSections:
    "grid min-w-0 gap-4 p-4 [&>section]:min-w-0 [&>section]:rounded-xl [&>section]:border [&>section]:border-[var(--border)] [&>section]:bg-[var(--bg-raised)] [&>section]:p-4 [&>section]:shadow-sm max-[520px]:gap-3 max-[520px]:p-3 max-[520px]:[&>section]:p-3",
  managerLayout:
    "grid min-h-[420px] min-w-0 grid-cols-[minmax(180px,0.38fr)_minmax(0,1fr)] overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-raised)] shadow-sm max-[760px]:grid-cols-1 max-[760px]:grid-rows-[minmax(132px,34%)_minmax(0,1fr)]",
  managerList:
    "min-h-0 min-w-0 overflow-y-auto border-r border-[var(--border)] bg-[var(--bg-workspace)] p-2 max-[760px]:border-b max-[760px]:border-r-0",
  group:
    "grid min-w-0 gap-1 border-b border-[var(--border)] py-2 last:border-b-0 [&>header]:flex [&>header]:min-h-8 [&>header]:items-center [&>header]:justify-between [&>header]:px-2 [&>header]:text-xs [&>header]:font-semibold [&>header]:text-[var(--text-muted)] [&>button]:flex [&>button]:min-h-10 [&>button]:w-full [&>button]:min-w-0 [&>button]:items-center [&>button]:justify-between [&>button]:gap-2 [&>button]:rounded-lg [&>button]:border-0 [&>button]:bg-transparent [&>button]:px-2.5 [&>button]:py-2 [&>button]:text-left [&>button]:text-sm [&>button]:text-[var(--text-secondary)] [&>button:hover]:bg-[var(--bg-hover)] [&>button:hover]:text-[var(--text)] [&>button:focus-visible]:outline-2 [&>button:focus-visible]:outline-[var(--text)] [&>button:disabled]:cursor-not-allowed [&>button:disabled]:opacity-50",
  managerEditor:
    "min-h-0 min-w-0 overflow-y-auto bg-[var(--bg-raised)] p-5 max-[520px]:p-4",
  formGrid:
    "grid min-w-0 grid-cols-2 gap-4 max-[760px]:grid-cols-1",
  card:
    "min-w-0 rounded-xl border border-[var(--border)] bg-[var(--bg-raised)] p-4 shadow-sm",
  row:
    "flex min-h-11 min-w-0 items-center justify-between gap-4 border-b border-[var(--border)] py-3 last:border-b-0",
  field:
    "grid min-w-0 gap-1.5 text-sm text-[var(--text-secondary)] [&>span]:font-medium [&>strong]:font-medium [&>small]:min-h-[1lh] [&>small]:text-xs [&>small]:leading-relaxed [&>small]:text-[var(--text-muted)]",
  input:
    "min-h-11 min-w-0 rounded-lg border border-[var(--border-strong)] bg-[var(--bg-workspace)] px-3 text-sm text-[var(--text)] outline-2 outline-transparent outline-offset-1 placeholder:text-[var(--text-muted)] hover:bg-[var(--bg-hover)] focus-visible:border-[var(--text-secondary)] focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50",
  textarea:
    "min-h-24 min-w-0 resize-y rounded-lg border border-[var(--border-strong)] bg-[var(--bg-workspace)] px-3 py-2.5 text-sm leading-relaxed text-[var(--text)] outline-2 outline-transparent outline-offset-1 placeholder:text-[var(--text-muted)] hover:bg-[var(--bg-hover)] focus-visible:border-[var(--text-secondary)] focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50",
  select:
    "min-h-11 min-w-0 rounded-lg border border-[var(--border-strong)] bg-[var(--bg-workspace)] px-3 text-sm text-[var(--text)] outline-2 outline-transparent outline-offset-1 hover:bg-[var(--bg-hover)] focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50",
  menu:
    "z-[100] grid min-w-48 gap-0.5 overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-menu)] p-1.5 text-sm text-[var(--text-secondary)] shadow-xl [&_button]:flex [&_button]:min-h-9 [&_button]:w-full [&_button]:items-center [&_button]:gap-2 [&_button]:whitespace-nowrap [&_button]:rounded-lg [&_button]:border-0 [&_button]:bg-transparent [&_button]:px-2.5 [&_button]:text-left [&_button]:text-sm [&_button]:text-[var(--text-secondary)] [&_button:hover]:bg-[var(--bg-hover)] [&_button:hover]:text-[var(--text)] [&_button:active]:bg-[var(--bg-active)] [&_button:disabled]:cursor-not-allowed [&_button:disabled]:opacity-50",
  menuSurface:
    "z-[100] overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-menu)] p-1.5 text-sm text-[var(--text-secondary)] shadow-xl [&_button]:min-h-9 [&_button]:whitespace-nowrap [&_button]:rounded-lg [&_button]:border-0 [&_button]:bg-transparent [&_button]:text-[var(--text-secondary)] [&_button:hover]:bg-[var(--bg-hover)] [&_button:hover]:text-[var(--text)] [&_button:active]:bg-[var(--bg-active)] [&_button:disabled]:cursor-not-allowed [&_button:disabled]:opacity-50",
  menuItem:
    "flex min-h-9 min-w-0 items-center gap-2 whitespace-nowrap rounded-lg border-0 bg-transparent px-2.5 text-left text-sm text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50 pointer-coarse:min-h-11",
  list:
    "min-h-0 min-w-0 overflow-y-auto",
  listItem:
    "flex min-h-10 min-w-0 items-center gap-2 rounded-lg border border-transparent px-3 py-2 text-left text-sm text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50 pointer-coarse:min-h-11",
  tab:
    "relative inline-flex min-h-10 shrink-0 items-center justify-center whitespace-nowrap border-0 bg-transparent px-3 text-sm font-medium text-[var(--text-secondary)] after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:scale-x-0 after:bg-[var(--text)] after:transition-transform after:duration-150 hover:text-[var(--text)] active:bg-[var(--bg-hover)] focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50 aria-selected:text-[var(--text)] aria-selected:after:scale-x-100 pointer-coarse:min-h-11",
  empty:
    "grid min-h-28 place-items-center gap-2 px-5 py-8 text-center text-sm leading-relaxed text-[var(--text-muted)]",
  status:
    "flex min-w-0 items-start gap-2 rounded-lg border border-[var(--border)] bg-[var(--bg-workspace)] px-3 py-2 text-sm leading-relaxed text-[var(--text-secondary)]",
  messageItem:
    "min-w-0 border-b border-[var(--border)] py-3 last:border-b-0 [&>header]:mb-2 [&>header]:flex [&>header]:items-center [&>header]:justify-between [&>header]:gap-3 [&>pre]:m-0 [&>pre]:whitespace-pre-wrap [&>pre]:font-body [&>pre]:text-sm [&>pre]:leading-relaxed",
  code:
    "overflow-auto rounded-lg border border-[var(--border)] bg-[var(--bg-code)] p-3 font-mono text-xs leading-relaxed text-[var(--text-code)]",
} as const;
