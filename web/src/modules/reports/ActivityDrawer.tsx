// Activity & processes drawer (owned by the ai-engine module).
export default function ActivityDrawer({ onClose }: { onClose: () => void }) {
  return <><div className="drw-ov on" onClick={onClose} /><aside className="drw on"><div className="drw-h"><h3>Activity</h3></div></aside></>
}
