import { useId, useState } from 'react'

type Props = {
  text: string
  label?: string
}

/** Circle "?" — hover/focus shows a description plaque. */
export default function HelpTip({ text, label = 'Подсказка' }: Props) {
  const id = useId()
  const [open, setOpen] = useState(false)

  return (
    <span
      className={`help-tip${open ? ' open' : ''}`}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      <button
        type="button"
        className="help-tip-btn"
        aria-label={label}
        aria-describedby={open ? id : undefined}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onClick={(e) => {
          e.preventDefault()
          e.stopPropagation()
          setOpen((v) => !v)
        }}
      >
        ?
      </button>
      {open ? (
        <span id={id} className="help-tip-plaque" role="tooltip">
          {text}
        </span>
      ) : null}
    </span>
  )
}
