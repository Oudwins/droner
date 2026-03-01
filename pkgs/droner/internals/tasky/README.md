

## Example usage

```go
	job := tasky.Job[string]{
		ID: tasky.JobID[string]{Value: "test"},
		Run: func(ctx context.Context, task *tasky.Task[string]) error {
			fmt.Printf("Handled task payload: %s\n", task.Payload)
			return nil
		},
	}
	backend, err := taskysqlite3.New[string](taskysqlite3.Config{
		Path:      "tasky_cli.db",
		QueueName: "tasky_cli_queue",
	})
	if err != nil {
		panic(err)
	}

	q, err := tasky.NewQueue(tasky.QueueConfig[string]{
		Jobs:    []tasky.Job[string]{job},
		Backend: backend,
		OnError: func(err error, task tasky.Task[string], taskID tasky.TaskID, payload []byte) error {
			fmt.Printf("tasky error: %v\n", err)
			return nil
		},
	})
	if err != nil {
		panic(err)
	}

	cmd := &cobra.Command{
		Use:   "tasky",
		Short: "..",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				for {
					jobID, taskID, payload, err := backend.Dequeue(ctx)
					if err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							return
						}
						fmt.Printf("dequeue error: %v\n", err)
						continue
					}
					fmt.Printf("task %v payload %s\n", taskID, payload)
					if jobID != job.ID {
						_ = backend.Ack(ctx, taskID)
						continue
					}
					if err := job.Run(ctx, &tasky.Task[string]{
						JobID:   jobID,
						TaskID:  taskID,
						Payload: payload,
					}); err != nil {
						_ = backend.Nack(ctx, taskID)
						continue
					}
					_ = backend.Ack(ctx, taskID)
				}
			}()

			reader := bufio.NewReader(os.Stdin)
			for {
				fmt.Print("tasky> ")
				line, err := reader.ReadString('\n')
				if err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
						return nil
					}
					return err
				}
				message := strings.TrimSpace(line)
				if message == "" {
					continue
				}
				if message == "exit" || message == "quit" {
					return nil
				}
				_, err = q.Enqueue(ctx, &tasky.Task[string]{
					JobID:   tasky.JobID[string]{Value: "test"},
					Payload: []byte(message),
				})
				if err != nil {
					fmt.Printf("enqueue error: %v\n", err)
				}
			}
			return nil
		},
	}
	return cmd

```
