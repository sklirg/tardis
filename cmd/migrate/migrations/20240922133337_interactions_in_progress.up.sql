CREATE TABLE interaction_in_progress (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    data jsonb,
    timestamp timestamp with time zone DEFAULT now()
);
