export default function TrafficMarks({
  drop,
  decline,
  dropHint,
  declineHint,
}: {
  drop?: boolean
  decline?: boolean
  dropHint?: string
  declineHint?: string
}) {
  if (!drop && !decline) return null
  return (
    <span className="traffic-marks">
      {drop ? (
        <span className="traffic-mark drop" title={dropHint || 'Резкое падение трафика'}>
          !
        </span>
      ) : null}
      {decline ? (
        <span
          className="traffic-mark decline"
          title={declineHint || 'Трафик ниже среднего по прошлым дням'}
        >
          <svg viewBox="0 0 16 14" width="15" height="13" aria-hidden>
            <path
              d="M8 1.4 14.6 13H1.4L8 1.4Z"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.4"
              strokeLinejoin="round"
            />
            <path d="M8 5.2v3.3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
            <circle cx="8" cy="10.6" r="0.7" fill="currentColor" />
          </svg>
        </span>
      ) : null}
    </span>
  )
}
