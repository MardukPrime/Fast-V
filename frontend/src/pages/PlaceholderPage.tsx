import Icon from "../components/Icon";

type PlaceholderProps = {
  title: string;
  description: string;
};

export default function PlaceholderPage(props: PlaceholderProps) {
  return (
    <section class="panel placeholder-page">
      <div class="placeholder-icon"><Icon name="route" size={34} /></div>
      <h2>{props.title}</h2>
      <p>{props.description}</p>
      <span class="tag neutral">后续里程碑</span>
    </section>
  );
}